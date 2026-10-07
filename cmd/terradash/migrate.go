package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"

	"github.com/alexmchughdev/terraform-provider-terradash/internal/migrate"
)

func runMigrate(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: terradash migrate [flags] DIR\n\nConverts grafana_dashboard resources in the module at DIR to terradash_dashboard,\nwith removed and import blocks so no dashboard is destroyed or recreated.\n\nFlags:")
		fs.PrintDefaults()
	}
	out := fs.String("o", "", "output .tf file or directory (default stdout)")
	remove := fs.Bool("remove", false, "delete the migrated grafana_dashboard blocks from DIR")
	force := fs.Bool("force", false, "overwrite existing files")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("expected exactly one module directory")
	}
	res, err := migrate.Run(fs.Arg(0))
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(stderr, "warning:", w)
	}
	if len(res.Outputs) == 0 {
		return errors.New("no grafana_dashboard resources could be migrated")
	}
	if err := write(res.Outputs, *out, *force, stdout, stderr); err != nil {
		return err
	}
	if *remove {
		return removeSources(res.Sources)
	}
	return nil
}

func removeSources(sources []migrate.Source) error {
	byFile := map[string][]string{}
	for _, s := range sources {
		byFile[s.File] = append(byFile[s.File], s.Name)
	}
	for file, names := range byFile {
		if err := removeBlocks(file, names); err != nil {
			return err
		}
	}
	return nil
}

func removeBlocks(file string, names []string) error {
	src, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	f, diags := hclwrite.ParseConfig(src, file, hcl.InitialPos)
	if diags.HasErrors() {
		return diags
	}
	for _, name := range names {
		if block := f.Body().FirstMatchingBlock("resource", []string{"grafana_dashboard", name}); block != nil {
			f.Body().RemoveBlock(block)
		}
	}
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	return os.WriteFile(file, hclwrite.Format(f.Bytes()), info.Mode().Perm())
}
