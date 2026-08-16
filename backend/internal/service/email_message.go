package service

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
)

type smtpMessage struct {
	envelopeFrom string
	envelopeTo   string
	data         []byte
}

func buildSMTPMessage(config *SMTPConfig, to, subject, body string) (smtpMessage, error) {
	if config == nil {
		return smtpMessage{}, errors.New("missing SMTP configuration")
	}

	fromAddress, err := parseSMTPAddress(config.From, "from")
	if err != nil {
		return smtpMessage{}, err
	}
	recipientAddress, err := parseSMTPAddress(to, "recipient")
	if err != nil {
		return smtpMessage{}, err
	}
	var replyToAddress *mail.Address
	if strings.TrimSpace(config.ReplyTo) != "" {
		replyToAddress, err = parseSMTPAddress(config.ReplyTo, "reply-to")
		if err != nil {
			return smtpMessage{}, err
		}
	}
	messageID, err := generateEmailMessageID(fromAddress.Address, config.Host)
	if err != nil {
		return smtpMessage{}, fmt.Errorf("generate message ID: %w", err)
	}

	fromName := sanitizeEmailHeader(config.FromName)
	if strings.TrimSpace(fromName) == "" {
		fromName = fromAddress.Name
	}
	fromHeader := (&mail.Address{
		Name:    fromName,
		Address: fromAddress.Address,
	}).String()
	toHeader := (&mail.Address{
		Name:    recipientAddress.Name,
		Address: recipientAddress.Address,
	}).String()
	subjectHeader := mime.QEncoding.Encode("UTF-8", sanitizeEmailHeader(subject))

	var message bytes.Buffer
	fmt.Fprintf(&message, "From: %s\r\n", fromHeader)
	fmt.Fprintf(&message, "To: %s\r\n", toHeader)
	if replyToAddress != nil {
		fmt.Fprintf(&message, "Reply-To: %s\r\n", replyToAddress.String())
	}
	fmt.Fprintf(&message, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&message, "Message-ID: %s\r\n", messageID)
	fmt.Fprintf(&message, "Subject: %s\r\n", subjectHeader)

	multipartWriter := multipart.NewWriter(&message)
	fmt.Fprint(&message, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&message, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", multipartWriter.Boundary())

	plainBody := htmlToPlainText(body)
	if strings.TrimSpace(plainBody) == "" {
		plainBody = "This message is available in HTML format."
	}
	if err := writeSMTPAlternativePart(multipartWriter, "text/plain", plainBody); err != nil {
		return smtpMessage{}, err
	}
	if err := writeSMTPAlternativePart(multipartWriter, "text/html", body); err != nil {
		return smtpMessage{}, err
	}
	if err := multipartWriter.Close(); err != nil {
		return smtpMessage{}, fmt.Errorf("close multipart email body: %w", err)
	}

	return smtpMessage{
		envelopeFrom: fromAddress.Address,
		envelopeTo:   recipientAddress.Address,
		data:         message.Bytes(),
	}, nil
}

func writeSMTPAlternativePart(writer *multipart.Writer, mediaType, body string) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Type", mediaType+`; charset="UTF-8"`)
	header.Set("Content-Transfer-Encoding", "quoted-printable")
	part, err := writer.CreatePart(header)
	if err != nil {
		return fmt.Errorf("create %s email part: %w", mediaType, err)
	}
	bodyWriter := quotedprintable.NewWriter(part)
	if _, err := bodyWriter.Write([]byte(body)); err != nil {
		return fmt.Errorf("encode %s email part: %w", mediaType, err)
	}
	if err := bodyWriter.Close(); err != nil {
		return fmt.Errorf("close %s email part encoder: %w", mediaType, err)
	}
	return nil
}

func htmlToPlainText(body string) string {
	doc, err := xhtml.Parse(strings.NewReader(body))
	if err != nil {
		return strings.TrimSpace(body)
	}

	var raw strings.Builder
	var walk func(*xhtml.Node, bool)
	walk = func(node *xhtml.Node, skip bool) {
		if node.Type == xhtml.ElementNode {
			switch node.Data {
			case "head", "script", "style", "svg":
				skip = true
			case "br":
				_ = raw.WriteByte('\n')
			}
			if !skip && isPlainTextBlock(node.Data) {
				_ = raw.WriteByte('\n')
			}
		}
		if node.Type == xhtml.TextNode && !skip {
			_, _ = raw.WriteString(node.Data)
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, skip)
		}

		if node.Type != xhtml.ElementNode || skip {
			return
		}
		if node.Data == "a" {
			if target := safePlainTextLink(node); target != "" {
				_, _ = raw.WriteString(" (")
				_, _ = raw.WriteString(target)
				_ = raw.WriteByte(')')
			}
		}
		if isPlainTextBlock(node.Data) {
			_ = raw.WriteByte('\n')
		}
	}
	walk(doc, false)

	lines := strings.Split(strings.ReplaceAll(raw.String(), "\r", ""), "\n")
	plainLines := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}
		plainLines = append(plainLines, line)
	}
	return strings.Join(plainLines, "\n")
}

func safePlainTextLink(node *xhtml.Node) string {
	for _, attr := range node.Attr {
		if !strings.EqualFold(attr.Key, "href") {
			continue
		}
		target := strings.TrimSpace(attr.Val)
		parsed, err := url.Parse(target)
		if err != nil {
			return ""
		}
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https", "mailto":
			return target
		default:
			return ""
		}
	}
	return ""
}

func isPlainTextBlock(tag string) bool {
	switch tag {
	case "address", "article", "aside", "blockquote", "div", "footer", "h1", "h2", "h3", "h4", "h5", "h6", "header", "li", "main", "nav", "p", "section", "table", "td", "th", "tr":
		return true
	default:
		return false
	}
}

func parseSMTPAddress(value, field string) (*mail.Address, error) {
	if strings.ContainsAny(value, "\r\n") {
		return nil, fmt.Errorf("invalid SMTP %s address: contains a line break", field)
	}

	cleaned := strings.TrimSpace(value)
	address, err := mail.ParseAddress(cleaned)
	if err != nil || strings.TrimSpace(address.Address) == "" {
		if err == nil {
			err = fmt.Errorf("address is empty")
		}
		return nil, fmt.Errorf("invalid SMTP %s address: %w", field, err)
	}
	return address, nil
}

func generateEmailMessageID(fromAddress, smtpHost string) (string, error) {
	randomID := make([]byte, 16)
	if _, err := rand.Read(randomID); err != nil {
		return "", err
	}

	domain := strings.TrimSpace(sanitizeEmailHeader(smtpHost))
	if at := strings.LastIndexByte(fromAddress, '@'); at >= 0 && at < len(fromAddress)-1 {
		domain = fromAddress[at+1:]
	}
	domain = strings.Trim(domain, "[]<>")
	if domain == "" {
		domain = "localhost"
	}

	return fmt.Sprintf("<%s@%s>", hex.EncodeToString(randomID), domain), nil
}
