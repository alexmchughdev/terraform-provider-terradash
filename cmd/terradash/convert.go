package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/alexmchughdev/terraform-provider-terradash/internal/convert"
	"github.com/alexmchughdev/terraform-provider-terradash/internal/dashboard"
)

func runConvert(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("o", "", "output .tf file or directory (default stdout)")
	importBlocks := fs.Bool("import", false, "emit import blocks for dashboards with a uid")
	folder := fs.String("folder-uid", "", "set folder_uid on every dashboard")
	name := fs.String("name", "", "resource name, for a single input")
	force := fs.Bool("force", false, "overwrite existing files")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	paths, err := expandInputs(fs.Args())
	if err != nil {
		return err
	}
	if *name != "" && len(paths) != 1 {
		return errors.New("-name requires exactly one input")
	}
	if *name != "" && !hclsyntax.ValidIdentifier(*name) {
		return fmt.Errorf("-name %q is not a valid Terraform identifier", *name)
	}
	if *name == "variables" {
		return errors.New(`-name "variables" is reserved for variables.tf`)
	}
	if *folder != "" && !dashboard.ValidUID(*folder) {
		return fmt.Errorf("-folder-uid %s", dashboard.UIDRule)
	}

	var namer convert.Namer
	var outputs []convert.Output
	for _, path := range paths {
		src, err := loadFile(path, stdin)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		resourceName := *name
		if resourceName == "" {
			resourceName = namer.Name(src.Title())
		}
		outputs = append(outputs, convert.Convert(src, resourceName, convert.Options{Import: *importBlocks, FolderUID: *folder}))
	}
	return write(outputs, *out, *force, stdout, stderr)
}

func expandInputs(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, errors.New("no input files (use - for stdin)")
	}
	stdinArgs := 0
	for _, arg := range args {
		if arg == "-" {
			stdinArgs++
		}
	}
	if stdinArgs > 1 {
		return nil, errors.New("stdin (-) can only be read once")
	}
	var paths []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		switch {
		case arg == "-" || (err == nil && !info.IsDir()):
			paths = append(paths, arg)
		case err != nil:
			return nil, err
		default:
			files, err := jsonFiles(arg)
			if err != nil {
				return nil, err
			}
			paths = append(paths, files...)
		}
	}
	return paths, nil
}

func jsonFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && isDashboardFile(e.Name()) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no .json, .yaml or .yml files", dir)
	}
	return files, nil
}

func isDashboardFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".json" || ext == ".yaml" || ext == ".yml"
}

func loadFile(path string, stdin io.Reader) (convert.Source, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return convert.Source{}, err
	}
	return convert.Load(data)
}
