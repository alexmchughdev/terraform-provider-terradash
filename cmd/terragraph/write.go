package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/alexmchughdev/terraform-provider-terragraph/internal/convert"
)

func write(outputs []convert.Output, out string, force bool, stdout, stderr io.Writer) error {
	if len(outputs) == 0 {
		fmt.Fprintln(stderr, "no dashboards found")
		return nil
	}
	vars := map[string]convert.Variable{}
	conflicts := map[string]bool{}
	for _, o := range outputs {
		for _, w := range o.Warnings {
			fmt.Fprintln(stderr, "warning:", w)
		}
		mergeVariables(vars, o.Variables, conflicts)
	}
	for _, name := range slices.Sorted(maps.Keys(conflicts)) {
		fmt.Fprintf(stderr, "warning: dashboards disagree on the default of variable %q; keeping the first\n", name)
	}
	if out == "" || strings.HasSuffix(out, ".tf") {
		return writeSingle(outputs, vars, out, force, stdout)
	}
	return writeDir(outputs, vars, out, force)
}

func mergeVariables(dst, src map[string]convert.Variable, conflicts map[string]bool) {
	for name, v := range src {
		existing, ok := dst[name]
		if !ok {
			dst[name] = v
			continue
		}
		if !reflect.DeepEqual(existing.Default, v.Default) {
			conflicts[name] = true
		}
	}
}

func writeSingle(outputs []convert.Output, vars map[string]convert.Variable, out string, force bool, stdout io.Writer) error {
	var parts [][]byte
	if len(vars) > 0 {
		parts = append(parts, convert.VariablesHCL(vars))
	}
	for _, o := range outputs {
		parts = append(parts, o.HCL)
	}
	data := bytes.Join(parts, []byte("\n"))
	if out == "" {
		_, err := stdout.Write(data)
		return err
	}
	return writeFile(out, data, force)
}

func writeDir(outputs []convert.Output, vars map[string]convert.Variable, dir string, force bool) error {
	files := map[string][]byte{}
	if len(vars) > 0 {
		files[filepath.Join(dir, "variables.tf")] = convert.VariablesHCL(vars)
	}
	for _, o := range outputs {
		files[filepath.Join(dir, o.Name+".tf")] = o.HCL
	}
	if !force {
		for _, path := range slices.Sorted(maps.Keys(files)) {
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists (use -force to overwrite)", path)
			}
		}
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	for _, path := range slices.Sorted(maps.Keys(files)) {
		if err := writeFile(path, files[path], force); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(path string, data []byte, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !force {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s already exists (use -force to overwrite)", path)
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return errors.Join(err, f.Close())
	}
	return f.Close()
}
