package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	expectedSHA256       = "98f18af50ae09fd00132f1d8501e3ff799c615fadf60123d08fbc1c0f5737e9f"
	expectedTotalRows    = 2030
	expectedEnabledRows  = 1869
	expectedFailedRows   = 161
	expectedWarningCount = 6
	defaultGroupName     = "Proxy-20260808"
	importAdvisoryLockID = int64(7368220260808)
)

var digitsOnly = regexp.MustCompile(`^[0-9]+$`)

type workbookCell struct {
	Ref    string `xml:"r,attr"`
	Type   string `xml:"t,attr"`
	Value  string `xml:"v"`
	Inline struct {
		Text string `xml:"t"`
	} `xml:"is"`
}

type workbookRow struct {
	Number int            `xml:"r,attr"`
	Cells  []workbookCell `xml:"c"`
}

type worksheetXML struct {
	Rows []workbookRow `xml:"sheetData>row"`
}

type sharedStringItem struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

type sharedStringsXML struct {
	Items []sharedStringItem `xml:"si"`
}

type importProxy struct {
	Row      int
	Name     string
	Protocol string
	Host     string
	Port     int
	Username string
	Password string
}

type importWarning struct {
	Row  int
	Code string
}

type parseResult struct {
	TotalRows int
	Enabled   int
	Failed    int
	Proxies   []importProxy
	Warnings  []importWarning
}

func main() {
	input := flag.String("input", "", "path to Proxy-20260808 XLSX file")
	emitSQL := flag.Bool("emit-sql", false, "emit a transactional psql COPY script to stdout")
	groupName := flag.String("group", defaultGroupName, "target proxy group name")
	flag.Parse()

	if strings.TrimSpace(*input) == "" {
		exitf("-input is required")
	}
	group := strings.TrimSpace(*groupName)
	if utf8.RuneCountInString(group) == 0 || utf8.RuneCountInString(group) > 100 {
		exitf("group name must contain 1-100 characters after trimming")
	}

	fileHash, err := fileSHA256(*input)
	if err != nil {
		exitf("hash input: %v", err)
	}
	if fileHash != expectedSHA256 {
		exitf("input SHA-256 mismatch: got %s", fileHash)
	}

	result, err := parseWorkbook(*input)
	if err != nil {
		exitf("parse workbook: %v", err)
	}
	if err := validateExpectedInput(result); err != nil {
		exitf("input validation failed: %v", err)
	}

	if *emitSQL {
		for _, warning := range result.Warnings {
			_, _ = fmt.Fprintf(os.Stderr, "warning row=%d code=%s\n", warning.Row, warning.Code)
		}
		if err := writeImportSQL(os.Stdout, group, result.Proxies); err != nil {
			exitf("emit SQL: %v", err)
		}
		return
	}

	protocols := make(map[string]int)
	for _, proxy := range result.Proxies {
		protocols[proxy.Protocol]++
	}
	fmt.Printf("sha256=%s total=%d enabled=%d failed=%d duplicates=0 warnings=%d\n",
		fileHash, result.TotalRows, result.Enabled, result.Failed, len(result.Warnings))
	keys := make([]string, 0, len(protocols))
	for protocol := range protocols {
		keys = append(keys, protocol)
	}
	sort.Strings(keys)
	for _, protocol := range keys {
		fmt.Printf("protocol_%s=%d\n", protocol, protocols[protocol])
	}
	for _, warning := range result.Warnings {
		fmt.Printf("warning row=%d code=%s\n", warning.Row, warning.Code)
	}
}

func exitf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func parseWorkbook(path string) (*parseResult, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = archive.Close() }()

	sharedStrings, err := readSharedStrings(archive.File)
	if err != nil {
		return nil, err
	}
	sheetData, err := readZipEntry(archive.File, "xl/worksheets/sheet1.xml")
	if err != nil {
		return nil, err
	}

	var worksheet worksheetXML
	if err := xml.Unmarshal(sheetData, &worksheet); err != nil {
		return nil, fmt.Errorf("decode first worksheet: %w", err)
	}
	return parseWorksheetRows(worksheet.Rows, sharedStrings)
}

func readSharedStrings(files []*zip.File) ([]string, error) {
	data, err := readZipEntry(files, "xl/sharedStrings.xml")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var table sharedStringsXML
	if err := xml.Unmarshal(data, &table); err != nil {
		return nil, fmt.Errorf("decode shared strings: %w", err)
	}
	values := make([]string, 0, len(table.Items))
	for _, item := range table.Items {
		var value strings.Builder
		_, _ = value.WriteString(item.Text)
		for _, run := range item.Runs {
			_, _ = value.WriteString(run.Text)
		}
		values = append(values, value.String())
	}
	return values, nil
}

func readZipEntry(files []*zip.File, name string) ([]byte, error) {
	cleanName := filepath.ToSlash(name)
	for _, file := range files {
		if filepath.ToSlash(file.Name) != cleanName {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return data, nil
	}
	return nil, fmt.Errorf("%w: %s", os.ErrNotExist, name)
}

func parseWorksheetRows(rows []workbookRow, sharedStrings []string) (*parseResult, error) {
	if len(rows) == 0 {
		return nil, errors.New("worksheet has no rows")
	}

	headerValues, err := rowValues(rows[0], sharedStrings)
	if err != nil {
		return nil, fmt.Errorf("read header row: %w", err)
	}
	headers := make(map[string]int, len(headerValues))
	for column, value := range headerValues {
		headers[strings.TrimSpace(value)] = column
	}
	requiredHeaders := []string{"Num", "ProxyAddr", "\u72b6\u6001", "\u8d26\u53f7"}
	for _, header := range requiredHeaders {
		if _, ok := headers[header]; !ok {
			return nil, fmt.Errorf("missing required header %q", header)
		}
	}

	result := &parseResult{Proxies: make([]importProxy, 0, len(rows)-1)}
	identities := make(map[string]int)
	names := make(map[string]int)
	for _, row := range rows[1:] {
		values, err := rowValues(row, sharedStrings)
		if err != nil {
			return nil, fmt.Errorf("read row %d: %w", row.Number, err)
		}
		if rowEmpty(values) {
			continue
		}
		result.TotalRows++
		status := strings.TrimSpace(valueAt(values, headers["\u72b6\u6001"]))
		switch status {
		case "\u542f\u7528":
			result.Enabled++
		case "\u5931\u8d25":
			result.Failed++
			continue
		default:
			return nil, fmt.Errorf("row %d has unsupported status %q", row.Number, status)
		}

		proxy, err := parseProxyRow(row.Number, valueAt(values, headers["Num"]), valueAt(values, headers["ProxyAddr"]))
		if err != nil {
			return nil, err
		}
		identity := proxyIdentity(proxy)
		if firstRow, exists := identities[identity]; exists {
			return nil, fmt.Errorf("rows %d and %d have duplicate proxy identity", firstRow, row.Number)
		}
		identities[identity] = row.Number
		if firstRow, exists := names[proxy.Name]; exists {
			return nil, fmt.Errorf("rows %d and %d generate duplicate proxy name %q", firstRow, row.Number, proxy.Name)
		}
		names[proxy.Name] = row.Number

		if strings.TrimSpace(valueAt(values, headers["\u8d26\u53f7"])) != "" && proxy.Username == "" {
			result.Warnings = append(result.Warnings, importWarning{Row: row.Number, Code: "account_without_url_auth"})
		}
		result.Proxies = append(result.Proxies, proxy)
	}
	return result, nil
}

func rowValues(row workbookRow, sharedStrings []string) ([]string, error) {
	values := make([]string, 0)
	for _, cell := range row.Cells {
		column, err := cellColumn(cell.Ref)
		if err != nil {
			return nil, err
		}
		for len(values) <= column {
			values = append(values, "")
		}
		value := cell.Value
		switch cell.Type {
		case "s":
			index, err := strconv.Atoi(strings.TrimSpace(cell.Value))
			if err != nil || index < 0 || index >= len(sharedStrings) {
				return nil, fmt.Errorf("cell %s has invalid shared string index %q", cell.Ref, cell.Value)
			}
			value = sharedStrings[index]
		case "inlineStr":
			value = cell.Inline.Text
		}
		values[column] = value
	}
	return values, nil
}

func cellColumn(ref string) (int, error) {
	column := 0
	letters := 0
	for _, r := range ref {
		if r >= 'A' && r <= 'Z' {
			column = column*26 + int(r-'A'+1)
			letters++
			continue
		}
		if r >= 'a' && r <= 'z' {
			column = column*26 + int(r-'a'+1)
			letters++
			continue
		}
		break
	}
	if letters == 0 {
		return 0, fmt.Errorf("invalid cell reference %q", ref)
	}
	return column - 1, nil
}

func rowEmpty(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func valueAt(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}

func parseProxyRow(row int, rawNum, rawAddress string) (importProxy, error) {
	num := strings.TrimSpace(rawNum)
	if !digitsOnly.MatchString(num) {
		return importProxy{}, fmt.Errorf("row %d has invalid Num %q", row, num)
	}
	parsedNum, err := strconv.ParseInt(num, 10, 64)
	if err != nil || parsedNum <= 0 {
		return importProxy{}, fmt.Errorf("row %d has invalid Num %q", row, num)
	}

	address := strings.TrimSpace(rawAddress)
	parsed, err := url.Parse(address)
	if err != nil {
		return importProxy{}, fmt.Errorf("row %d has invalid ProxyAddr: %w", row, err)
	}
	protocol := strings.ToLower(parsed.Scheme)
	if protocol != "http" && protocol != "https" && protocol != "socks5" && protocol != "socks5h" {
		return importProxy{}, fmt.Errorf("row %d has unsupported proxy protocol %q", row, parsed.Scheme)
	}
	if parsed.Opaque != "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return importProxy{}, fmt.Errorf("row %d ProxyAddr contains unsupported path, query, or fragment", row)
	}
	host := parsed.Hostname()
	if host == "" || len(host) > 255 {
		return importProxy{}, fmt.Errorf("row %d has invalid proxy host", row)
	}
	portText := parsed.Port()
	if portText == "" {
		switch protocol {
		case "http":
			portText = "80"
		case "https":
			portText = "443"
		default:
			return importProxy{}, fmt.Errorf("row %d has no proxy port", row)
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return importProxy{}, fmt.Errorf("row %d has invalid proxy port", row)
	}

	username := ""
	password := ""
	if parsed.User != nil {
		username = parsed.User.Username()
		var hasPassword bool
		password, hasPassword = parsed.User.Password()
		if username == "" || !hasPassword || password == "" {
			return importProxy{}, fmt.Errorf("row %d has incomplete proxy authentication", row)
		}
	}
	if len(username) > 100 || len(password) > 100 {
		return importProxy{}, fmt.Errorf("row %d proxy authentication exceeds 100 characters", row)
	}
	name := fmt.Sprintf("Proxy-20260808-%d", parsedNum)
	if len(name) > 100 {
		return importProxy{}, fmt.Errorf("row %d generated proxy name is too long", row)
	}

	return importProxy{
		Row:      row,
		Name:     name,
		Protocol: protocol,
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
	}, nil
}

func proxyIdentity(proxy importProxy) string {
	return strings.Join([]string{proxy.Host, strconv.Itoa(proxy.Port), proxy.Username, proxy.Password}, "\x00")
}

func validateExpectedInput(result *parseResult) error {
	if result.TotalRows != expectedTotalRows {
		return fmt.Errorf("total rows: got %d, want %d", result.TotalRows, expectedTotalRows)
	}
	if result.Enabled != expectedEnabledRows || len(result.Proxies) != expectedEnabledRows {
		return fmt.Errorf("enabled rows: got %d parsed %d, want %d", result.Enabled, len(result.Proxies), expectedEnabledRows)
	}
	if result.Failed != expectedFailedRows {
		return fmt.Errorf("failed rows: got %d, want %d", result.Failed, expectedFailedRows)
	}
	if len(result.Warnings) != expectedWarningCount {
		return fmt.Errorf("warnings: got %d, want %d", len(result.Warnings), expectedWarningCount)
	}
	return nil
}

func writeImportSQL(w io.Writer, groupName string, proxies []importProxy) error {
	if len(proxies) != expectedEnabledRows {
		return fmt.Errorf("refusing to emit %d rows; expected %d", len(proxies), expectedEnabledRows)
	}
	groupLiteral := quoteSQLLiteral(groupName)
	header := fmt.Sprintf(`\set ON_ERROR_STOP on
BEGIN;
SELECT pg_advisory_xact_lock(%d);
LOCK TABLE proxy_groups, proxies IN SHARE ROW EXCLUSIVE MODE;
CREATE TEMP TABLE proxy_pool_import_stage (
  source_row integer NOT NULL,
  name varchar(100) NOT NULL,
  protocol varchar(20) NOT NULL,
  host varchar(255) NOT NULL,
  port integer NOT NULL,
  username varchar(100) NOT NULL,
  password varchar(100) NOT NULL
) ON COMMIT DROP;
\copy proxy_pool_import_stage (source_row, name, protocol, host, port, username, password) FROM STDIN WITH (FORMAT csv)
`, importAdvisoryLockID)
	if _, err := io.WriteString(w, header); err != nil {
		return err
	}
	csvWriter := csv.NewWriter(w)
	for _, proxy := range proxies {
		record := []string{
			strconv.Itoa(proxy.Row), proxy.Name, proxy.Protocol, proxy.Host,
			strconv.Itoa(proxy.Port), proxy.Username, proxy.Password,
		}
		if err := csvWriter.Write(record); err != nil {
			return err
		}
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return err
	}

	footer := fmt.Sprintf(`\.
DO $validation$
BEGIN
  IF (SELECT COUNT(*) FROM proxy_pool_import_stage) <> %d THEN
    RAISE EXCEPTION 'staging row count mismatch';
  END IF;
  IF EXISTS (
    SELECT 1 FROM proxy_pool_import_stage
    GROUP BY host, port, username, password HAVING COUNT(*) > 1
  ) THEN
    RAISE EXCEPTION 'staging contains duplicate proxy identities';
  END IF;
END
$validation$;

CREATE TEMP TABLE proxy_pool_import_baseline ON COMMIT DROP AS
SELECT
  (SELECT COUNT(*) FROM proxies WHERE deleted_at IS NULL) AS proxy_count,
  (SELECT COUNT(*) FROM proxies p
    JOIN proxy_groups g ON g.id = p.proxy_group_id
    WHERE p.deleted_at IS NULL AND lower(g.name) = lower(%s)) AS target_group_count,
  (SELECT COUNT(*) FROM (
    SELECT host, port, COALESCE(username, ''), COALESCE(password, '')
    FROM proxies WHERE deleted_at IS NULL
    GROUP BY 1, 2, 3, 4 HAVING COUNT(*) > 1
  ) duplicates) AS duplicate_groups;

INSERT INTO proxy_groups (name, created_at, updated_at)
SELECT %s, NOW(), NOW()
WHERE NOT EXISTS (SELECT 1 FROM proxy_groups WHERE lower(name) = lower(%s));

CREATE TEMP TABLE proxy_pool_import_result (created integer NOT NULL, skipped integer NOT NULL) ON COMMIT DROP;
WITH target_group AS (
  SELECT id FROM proxy_groups WHERE lower(name) = lower(%s)
), inserted AS (
  INSERT INTO proxies (
    name, protocol, host, port, username, password, status,
    fallback_mode, expiry_warn_days, proxy_group_id, created_at, updated_at
  )
  SELECT
    s.name, s.protocol, s.host, s.port, NULLIF(s.username, ''), NULLIF(s.password, ''), 'active',
    'none', 7, g.id, NOW(), NOW()
  FROM proxy_pool_import_stage s
  CROSS JOIN target_group g
  WHERE NOT EXISTS (
    SELECT 1 FROM proxies p
    WHERE p.deleted_at IS NULL
      AND p.host = s.host
      AND p.port = s.port
      AND COALESCE(p.username, '') = s.username
      AND COALESCE(p.password, '') = s.password
  )
  RETURNING 1
)
INSERT INTO proxy_pool_import_result (created, skipped)
SELECT COUNT(*)::integer, (%d - COUNT(*))::integer FROM inserted;

DO $verification$
DECLARE
  result proxy_pool_import_result%%ROWTYPE;
  baseline proxy_pool_import_baseline%%ROWTYPE;
  current_proxy_count bigint;
  current_group_count bigint;
  current_duplicate_groups bigint;
BEGIN
  SELECT * INTO result FROM proxy_pool_import_result;
  SELECT * INTO baseline FROM proxy_pool_import_baseline;
  SELECT COUNT(*) INTO current_proxy_count FROM proxies WHERE deleted_at IS NULL;
  SELECT COUNT(*) INTO current_group_count
    FROM proxies p JOIN proxy_groups g ON g.id = p.proxy_group_id
    WHERE p.deleted_at IS NULL AND lower(g.name) = lower(%s);
  SELECT COUNT(*) INTO current_duplicate_groups FROM (
    SELECT host, port, COALESCE(username, ''), COALESCE(password, '')
    FROM proxies WHERE deleted_at IS NULL
    GROUP BY 1, 2, 3, 4 HAVING COUNT(*) > 1
  ) duplicates;
  IF result.created + result.skipped <> %d THEN
    RAISE EXCEPTION 'created + skipped verification failed';
  END IF;
  IF current_proxy_count <> baseline.proxy_count + result.created THEN
    RAISE EXCEPTION 'proxy count verification failed';
  END IF;
  IF current_group_count <> baseline.target_group_count + result.created THEN
    RAISE EXCEPTION 'target group count verification failed';
  END IF;
  IF current_duplicate_groups <> baseline.duplicate_groups THEN
    RAISE EXCEPTION 'identity duplicate baseline changed';
  END IF;
END
$verification$;

SELECT
  'created=' || created || ' skipped=' || skipped ||
  ' target_group_total=' || (
    SELECT COUNT(*) FROM proxies p JOIN proxy_groups g ON g.id = p.proxy_group_id
    WHERE p.deleted_at IS NULL AND lower(g.name) = lower(%s)
  )
FROM proxy_pool_import_result;
COMMIT;
`, expectedEnabledRows,
		groupLiteral, groupLiteral, groupLiteral, groupLiteral,
		expectedEnabledRows, groupLiteral, expectedEnabledRows, groupLiteral)
	_, err := io.Copy(w, bytes.NewBufferString(footer))
	return err
}

func quoteSQLLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
