package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseWorkbookIgnoresWorksheetDimension(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pool.xlsx")
	file, err := os.Create(path)
	require.NoError(t, err)
	archive := zip.NewWriter(file)
	writeZipFile(t, archive, "xl/sharedStrings.xml", `<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <si><t>Num</t></si><si><t>ProxyAddr</t></si><si><t>&#x72B6;&#x6001;</t></si><si><t>&#x8D26;&#x53F7;</t></si>
  <si><t>&#x542F;&#x7528;</t></si><si><t>&#x5931;&#x8D25;</t></si>
  <si><t>socks5://user:pass@example.com:1080</t></si><si><t>user</t></si>
</sst>`)
	writeZipFile(t, archive, "xl/worksheets/sheet1.xml", `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <dimension ref="A1"/>
  <sheetData>
    <row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c><c r="D1" t="s"><v>3</v></c></row>
    <row r="2"><c r="A2"><v>1</v></c><c r="B2" t="s"><v>6</v></c><c r="C2" t="s"><v>4</v></c><c r="D2" t="s"><v>7</v></c></row>
    <row r="3"><c r="A3"><v>2</v></c><c r="C3" t="s"><v>5</v></c></row>
  </sheetData>
</worksheet>`)
	require.NoError(t, archive.Close())
	require.NoError(t, file.Close())

	result, err := parseWorkbook(path)
	require.NoError(t, err)
	require.Equal(t, 2, result.TotalRows)
	require.Equal(t, 1, result.Enabled)
	require.Equal(t, 1, result.Failed)
	require.Empty(t, result.Warnings)
	require.Equal(t, []importProxy{{
		Row: 2, Name: "Proxy-20260808-1", Protocol: "socks5", Host: "example.com",
		Port: 1080, Username: "user", Password: "pass",
	}}, result.Proxies)
}

func TestParseWorksheetRowsWarnsWhenSideAccountIsNotInURL(t *testing.T) {
	t.Parallel()

	rows := []workbookRow{
		{Number: 1, Cells: []workbookCell{
			inlineCell("A1", "Num"), inlineCell("B1", "ProxyAddr"),
			inlineCell("C1", "\u72b6\u6001"), inlineCell("D1", "\u8d26\u53f7"),
		}},
		{Number: 9, Cells: []workbookCell{
			inlineCell("A9", "9"), inlineCell("B9", "http://example.com:80"),
			inlineCell("C9", "\u542f\u7528"), inlineCell("D9", "side-column-user"),
		}},
	}

	result, err := parseWorksheetRows(rows, nil)
	require.NoError(t, err)
	require.Equal(t, []importWarning{{Row: 9, Code: "account_without_url_auth"}}, result.Warnings)
	require.Empty(t, result.Proxies[0].Username)
	require.Empty(t, result.Proxies[0].Password)
}

func TestParseWorksheetRowsRejectsDuplicateIdentity(t *testing.T) {
	t.Parallel()

	rows := []workbookRow{
		{Number: 1, Cells: []workbookCell{
			inlineCell("A1", "Num"), inlineCell("B1", "ProxyAddr"),
			inlineCell("C1", "\u72b6\u6001"), inlineCell("D1", "\u8d26\u53f7"),
		}},
		{Number: 2, Cells: []workbookCell{
			inlineCell("A2", "1"), inlineCell("B2", "http://user:pass@example.com:8080"), inlineCell("C2", "\u542f\u7528"),
		}},
		{Number: 3, Cells: []workbookCell{
			inlineCell("A3", "2"), inlineCell("B3", "https://user:pass@example.com:8080"), inlineCell("C3", "\u542f\u7528"),
		}},
	}

	_, err := parseWorksheetRows(rows, nil)
	require.ErrorContains(t, err, "duplicate proxy identity")
}

func TestParseWorksheetRowsRejectsDuplicateGeneratedName(t *testing.T) {
	t.Parallel()

	rows := []workbookRow{
		{Number: 1, Cells: []workbookCell{
			inlineCell("A1", "Num"), inlineCell("B1", "ProxyAddr"),
			inlineCell("C1", "\u72b6\u6001"), inlineCell("D1", "\u8d26\u53f7"),
		}},
		{Number: 2, Cells: []workbookCell{
			inlineCell("A2", "1"), inlineCell("B2", "http://first.example.test:8080"), inlineCell("C2", "\u542f\u7528"),
		}},
		{Number: 3, Cells: []workbookCell{
			inlineCell("A3", "1"), inlineCell("B3", "http://second.example.test:8080"), inlineCell("C3", "\u542f\u7528"),
		}},
	}

	_, err := parseWorksheetRows(rows, nil)
	require.ErrorContains(t, err, "duplicate proxy name")
}

func TestParseProxyRowRejectsUnsupportedURLPartsAndIncompleteAuth(t *testing.T) {
	t.Parallel()

	_, err := parseProxyRow(2, "1", "http://example.test:8080/path")
	require.ErrorContains(t, err, "unsupported path")

	_, err = parseProxyRow(3, "2", "http://user@example.test:8080")
	require.ErrorContains(t, err, "incomplete proxy authentication")
}

func TestWriteImportSQLUsesLockedInsertOnlyTransaction(t *testing.T) {
	t.Parallel()

	proxies := make([]importProxy, expectedEnabledRows)
	for i := range proxies {
		proxies[i] = importProxy{
			Row: i + 2, Name: "Proxy-20260808-" + strconv.Itoa(i+1), Protocol: "http",
			Host: "example-" + strconv.Itoa(i+1) + ".test", Port: 8080,
		}
	}
	var output bytes.Buffer
	require.NoError(t, writeImportSQL(&output, defaultGroupName, proxies))
	sql := output.String()
	require.Contains(t, sql, "BEGIN;")
	require.Contains(t, sql, "pg_advisory_xact_lock")
	require.Contains(t, sql, "LOCK TABLE proxy_groups, proxies IN SHARE ROW EXCLUSIVE MODE")
	require.Contains(t, sql, "\\copy proxy_pool_import_stage")
	require.Contains(t, sql, "COALESCE(p.username, '') = s.username")
	require.Contains(t, sql, "created + result.skipped")
	require.Contains(t, sql, "COMMIT;")
	require.NotContains(t, strings.ToUpper(sql), "UPDATE PROXIES")
}

func writeZipFile(t *testing.T, archive *zip.Writer, name, contents string) {
	t.Helper()
	entry, err := archive.Create(name)
	require.NoError(t, err)
	_, err = entry.Write([]byte(contents))
	require.NoError(t, err)
}

func inlineCell(ref, value string) workbookCell {
	cell := workbookCell{Ref: ref, Type: "inlineStr"}
	cell.Inline.Text = value
	return cell
}
