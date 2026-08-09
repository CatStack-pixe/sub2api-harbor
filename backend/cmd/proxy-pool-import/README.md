# Proxy Pool Import

This one-time tool validates and streams the `Proxy-20260808` workbook. It does
not use the proxy JSON API because that path probes every new proxy and can
modify matching existing proxies.

Download the Windows artifact from the successful `main` GitHub Actions run for
the exact commit being deployed. Verify `proxy-pool-import.exe` against the
included SHA-256 file before use. Do not build the importer locally.

Dry-run from the repository's `backend` directory:

```powershell
.\proxy-pool-import.exe -input ..\..\Proxy-20260808-031830.xlsx
```

The command refuses to continue unless the workbook SHA-256 and the expected
`2030 / 1869 / 161` row counts match. It reads worksheet cells directly and
does not trust the XLSX dimension metadata.

For production, pipe `-emit-sql` directly over SSH into `psql`. Do not save the
generated stream because it contains proxy credentials. The SQL takes an
advisory lock and table locks, imports through a temporary table, skips exact
`host + port + username + password` matches without updating them, and verifies
the aggregate result before commit.
