package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
)

type report struct {
	Schema           int               `json:"schema"`
	GoVersion        string            `json:"go_version"`
	Platform         string            `json:"platform"`
	VerificationTime string            `json:"verification_time"`
	Environment      map[string]string `json:"environment"`
	CoveredSPKIs     int               `json:"covered_spkis"`
	OfficialSPKIs    int               `json:"official_spkis"`
	Results          []checkResult     `json:"results"`
	ExitCode         int               `json:"exit_code"`
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("x509check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	check := flags.String("check", "all", "checks to run: all, crypto, roots")
	asJSON := flags.Bool("json", false, "emit a machine-readable report")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || (*check != "all" && *check != "crypto" && *check != "roots") {
		fmt.Fprintln(stderr, "usage: x509check [-check all|crypto|roots] [-json]")
		return 2
	}
	r := report{
		Schema: 1, GoVersion: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH,
		VerificationTime: verificationTime.Format("2006-01-02T15:04:05Z07:00"),
		Environment:      map[string]string{"SSL_CERT_FILE": os.Getenv("SSL_CERT_FILE"), "SSL_CERT_DIR": os.Getenv("SSL_CERT_DIR")},
		OfficialSPKIs:    len(distrustedSPKIs),
	}
	f, err := loadFixtures()
	if err != nil {
		r.Results = []checkResult{result("fixtures", "integrity", "error", err.Error())}
	} else {
		r.CoveredSPKIs = len(f.probes)
		r.Results = []checkResult{result("fixtures", "integrity", "pass", "embedded manifest, Go patch, PEM, DER and SPKI digests verified")}
		if *check == "all" || *check == "crypto" {
			r.Results = append(r.Results, checkCrypto(f)...)
		}
		if *check == "all" || *check == "roots" {
			r.Results = append(r.Results, systemRootChecks(f)...)
		}
	}
	r.ExitCode = exitCode(r.Results)
	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(r); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	} else {
		fmt.Fprintf(stdout, "x509check %s %s; fixed verification time %s; coverage %d/%d SPKIs\n",
			r.GoVersion, r.Platform, r.VerificationTime, r.CoveredSPKIs, r.OfficialSPKIs)
		fmt.Fprintf(stdout, "SSL_CERT_FILE=%q SSL_CERT_DIR=%q\n", r.Environment["SSL_CERT_FILE"], r.Environment["SSL_CERT_DIR"])
		for _, c := range r.Results {
			fmt.Fprintf(stdout, "%s %s/%s: %s\n", c.Status, c.Check, c.Name, c.Detail)
		}
		fmt.Fprintf(stdout, "exit_code=%d\n", r.ExitCode)
	}
	return r.ExitCode
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
