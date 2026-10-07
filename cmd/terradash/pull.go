package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/alexmchughdev/terraform-provider-terradash/internal/convert"
	"github.com/alexmchughdev/terraform-provider-terradash/internal/grafana"
)

func runPull(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: terradash pull [flags] [UID ...]\n\nCredentials are read from GRAFANA_AUTH (token, user:password or anonymous).\n\nFlags:")
		fs.PrintDefaults()
	}
	url := fs.String("url", os.Getenv("GRAFANA_URL"), "Grafana URL (default $GRAFANA_URL)")
	orgID := fs.String("org-id", os.Getenv("GRAFANA_ORG_ID"), "organization ID for basic auth (default $GRAFANA_ORG_ID)")
	caCert := fs.String("ca-cert", os.Getenv("GRAFANA_CA_CERT"), "path to a PEM CA bundle")
	insecure := fs.Bool("insecure-skip-verify", false, "skip TLS verification")
	all := fs.Bool("all", false, "export every dashboard")
	folders := fs.String("folder", "", "export dashboards in these comma-separated folder UIDs")
	out := fs.String("o", "", "output .tf file or directory (default stdout)")
	importBlocks := fs.Bool("import", true, "emit import blocks")
	force := fs.Bool("force", false, "overwrite existing files")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	client, err := newClient(*url, *orgID, *caCert, *insecure, stderr)
	if err != nil {
		return err
	}
	uids, err := selectUIDs(ctx, client, fs.Args(), *all, *folders)
	if err != nil {
		return err
	}

	var namer convert.Namer
	var outputs []convert.Output
	for _, uid := range uids {
		output, err := pullOne(ctx, client, uid, &namer, *importBlocks)
		if err != nil {
			return err
		}
		outputs = append(outputs, output)
	}
	return write(outputs, *out, *force, stdout, stderr)
}

func newClient(url, orgID, caCertPath string, insecure bool, stderr io.Writer) (*grafana.Client, error) {
	if url == "" {
		return nil, errors.New("set -url or GRAFANA_URL")
	}
	cfg := grafana.Config{
		URL:                url,
		Auth:               os.Getenv("GRAFANA_AUTH"),
		InsecureSkipVerify: insecure,
		Retries:            grafana.DefaultRetries,
		UserAgent:          "terradash/" + version,
	}
	if orgID != "" {
		id, err := strconv.ParseInt(orgID, 10, 64)
		if err != nil || id < 0 {
			return nil, fmt.Errorf("org id %q is not a non-negative integer", orgID)
		}
		cfg.OrgID = id
	}
	if caCertPath != "" {
		ca, err := os.ReadFile(caCertPath)
		if err != nil {
			return nil, err
		}
		cfg.CACertPEM = ca
	}
	if cfg.SendsPlaintextCredentials() {
		fmt.Fprintln(stderr, "warning: credentials are sent over plain HTTP")
	}
	return grafana.New(cfg)
}

func selectUIDs(ctx context.Context, client *grafana.Client, uids []string, all bool, folders string) ([]string, error) {
	if !all && folders == "" {
		if len(uids) == 0 {
			return nil, errors.New("specify dashboard UIDs, -all or -folder")
		}
		return uids, nil
	}
	var folderUIDs []string
	for _, f := range strings.Split(folders, ",") {
		if f = strings.TrimSpace(f); f != "" {
			folderUIDs = append(folderUIDs, f)
		}
	}
	if !all && len(folderUIDs) == 0 {
		return nil, errors.New("-folder needs at least one folder UID")
	}
	hits, err := client.SearchDashboards(ctx, folderUIDs)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, uid := range uids {
		seen[uid] = true
	}
	for _, h := range hits {
		if !seen[h.UID] {
			seen[h.UID] = true
			uids = append(uids, h.UID)
		}
	}
	return uids, nil
}

func pullOne(ctx context.Context, client *grafana.Client, uid string, namer *convert.Namer, importBlocks bool) (convert.Output, error) {
	dash, err := client.GetDashboard(ctx, uid)
	if err != nil {
		return convert.Output{}, err
	}
	src, err := convert.Check(convert.Source{Model: dash.Model, FolderUID: dash.Meta.FolderUID})
	if err != nil {
		return convert.Output{}, fmt.Errorf("%s: %w", uid, err)
	}
	output := convert.Convert(src, namer.Name(src.Title()), convert.Options{Import: importBlocks})
	if dash.Meta.Provisioned {
		output.Warnings = append(output.Warnings, fmt.Sprintf("%s: dashboard is provisioned from files; Grafana rejects API changes to it", uid))
	}
	return output, nil
}
