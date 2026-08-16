//go:build unit

package service

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildSMTPMessageProducesStandardsCompliantMIME(t *testing.T) {
	config := &SMTPConfig{
		Host:     "smtp.example.com",
		From:     "reply@example.com",
		FromName: "Sub2API 通知",
		ReplyTo:  "Support <support@example.com>",
	}
	body := "<html>\n<body>验证码：123456 &amp; ready</body>\n</html>"

	message, err := buildSMTPMessage(config, "User <user@example.net>", "邮箱验证码", body)
	require.NoError(t, err)
	require.Equal(t, "reply@example.com", message.envelopeFrom)
	require.Equal(t, "user@example.net", message.envelopeTo)

	parsed, err := mail.ReadMessage(bytes.NewReader(message.data))
	require.NoError(t, err)

	from, err := mail.ParseAddress(parsed.Header.Get("From"))
	require.NoError(t, err)
	require.Equal(t, "Sub2API 通知", from.Name)
	require.Equal(t, "reply@example.com", from.Address)

	recipient, err := mail.ParseAddress(parsed.Header.Get("To"))
	require.NoError(t, err)
	require.Equal(t, "User", recipient.Name)
	require.Equal(t, "user@example.net", recipient.Address)
	replyTo, err := mail.ParseAddress(parsed.Header.Get("Reply-To"))
	require.NoError(t, err)
	require.Equal(t, "Support", replyTo.Name)
	require.Equal(t, "support@example.com", replyTo.Address)

	decodedSubject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	require.NoError(t, err)
	require.Equal(t, "邮箱验证码", decodedSubject)
	require.NotEmpty(t, parsed.Header.Get("Date"))
	_, err = mail.ParseDate(parsed.Header.Get("Date"))
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^<[0-9a-f]{32}@example\.com>$`), parsed.Header.Get("Message-ID"))
	require.Equal(t, "1.0", parsed.Header.Get("MIME-Version"))
	require.Empty(t, parsed.Header.Get("Content-Transfer-Encoding"))

	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/alternative", mediaType)
	require.NotEmpty(t, params["boundary"])

	reader := multipart.NewReader(parsed.Body, params["boundary"])
	plainPart, err := reader.NextRawPart()
	require.NoError(t, err)
	require.Equal(t, "text/plain; charset=\"UTF-8\"", plainPart.Header.Get("Content-Type"))
	require.Equal(t, "quoted-printable", plainPart.Header.Get("Content-Transfer-Encoding"))
	plainBody, err := io.ReadAll(quotedprintable.NewReader(plainPart))
	require.NoError(t, err)
	require.Contains(t, string(plainBody), "123456 & ready")
	require.NotContains(t, string(plainBody), "<html>")

	htmlPart, err := reader.NextRawPart()
	require.NoError(t, err)
	require.Equal(t, "text/html; charset=\"UTF-8\"", htmlPart.Header.Get("Content-Type"))
	require.Equal(t, "quoted-printable", htmlPart.Header.Get("Content-Transfer-Encoding"))
	decodedBody, err := io.ReadAll(quotedprintable.NewReader(htmlPart))
	require.NoError(t, err)
	require.Equal(t, strings.ReplaceAll(body, "\n", "\r\n"), string(decodedBody))
	_, err = reader.NextRawPart()
	require.ErrorIs(t, err, io.EOF)
}

func TestBuildSMTPMessagePreventsHeaderInjection(t *testing.T) {
	config := &SMTPConfig{
		Host:     "smtp.example.com",
		From:     "reply@example.com",
		FromName: "Sender\r\nBcc: hidden@example.com",
	}

	message, err := buildSMTPMessage(config, "user@example.net", "Subject\r\nCc: hidden@example.com", "body")
	require.NoError(t, err)

	parsed, err := mail.ReadMessage(bytes.NewReader(message.data))
	require.NoError(t, err)
	require.Empty(t, parsed.Header.Get("Bcc"))
	require.Empty(t, parsed.Header.Get("Cc"))
	require.Empty(t, parsed.Header.Get("Reply-To"))

	decodedSubject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	require.NoError(t, err)
	require.Equal(t, "SubjectCc: hidden@example.com", decodedSubject)
}

func TestBuildSMTPMessageRejectsInvalidConfiguration(t *testing.T) {
	_, err := buildSMTPMessage(nil, "user@example.net", "subject", "body")
	require.ErrorContains(t, err, "missing SMTP configuration")

	_, err = buildSMTPMessage(&SMTPConfig{Host: "smtp.example.com"}, "user@example.net", "subject", "body")
	require.ErrorContains(t, err, "invalid SMTP from address")

	_, err = buildSMTPMessage(&SMTPConfig{
		Host: "smtp.example.com",
		From: "reply@example.com",
	}, "invalid recipient <>", "subject", "body")
	require.ErrorContains(t, err, "invalid SMTP recipient address")

	_, err = buildSMTPMessage(&SMTPConfig{
		Host: "smtp.example.com",
		From: "reply@example.com",
	}, "user@example.net\r\nBcc: hidden@example.net", "subject", "body")
	require.ErrorContains(t, err, "invalid SMTP recipient address")

	_, err = buildSMTPMessage(&SMTPConfig{
		Host:    "smtp.example.com",
		From:    "reply@example.com",
		ReplyTo: "support@example.net\r\nBcc: hidden@example.net",
	}, "user@example.net", "subject", "body")
	require.ErrorContains(t, err, "invalid SMTP reply-to address")
}

func TestHTMLToPlainTextPreservesUsefulLinksAndSkipsActiveContent(t *testing.T) {
	body := `<html><head><style>.hidden{display:none}</style></head><body><h1>Hello</h1><p>Reset your password:</p><a href="https://example.com/reset?token=abc">Reset</a><table><tr><th>Triggered at</th><td>2026-08-16</td></tr></table><script>alert(1)</script></body></html>`

	plain := htmlToPlainText(body)

	require.Equal(t, "Hello\nReset your password:\nReset (https://example.com/reset?token=abc)\nTriggered at\n2026-08-16", plain)
	require.NotContains(t, plain, "display:none")
	require.NotContains(t, plain, "alert(1)")
}

func TestBuildSMTPMessageUsesUniqueMessageIDs(t *testing.T) {
	config := &SMTPConfig{Host: "smtp.example.com", From: "reply@example.com"}

	first, err := buildSMTPMessage(config, "user@example.net", "subject", "body")
	require.NoError(t, err)
	second, err := buildSMTPMessage(config, "user@example.net", "subject", "body")
	require.NoError(t, err)

	firstParsed, err := mail.ReadMessage(bytes.NewReader(first.data))
	require.NoError(t, err)
	secondParsed, err := mail.ReadMessage(bytes.NewReader(second.data))
	require.NoError(t, err)
	require.NotEqual(t, firstParsed.Header.Get("Message-ID"), secondParsed.Header.Get("Message-ID"))

	_, firstParams, err := mime.ParseMediaType(firstParsed.Header.Get("Content-Type"))
	require.NoError(t, err)
	_, secondParams, err := mime.ParseMediaType(secondParsed.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.NotEqual(t, firstParams["boundary"], secondParams["boundary"])
}
