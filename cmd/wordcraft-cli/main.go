// Command wordcraft-cli is WordCraft's command line: the Go port of apps/wordcraft-cli, growing
// with the engine. Ported so far:
//
//	wordcraft-cli convert IN OUT
//	wordcraft-cli run --file IN [--cmd ID[=JSON]]… [--save OUT] [--print]
//	wordcraft-cli commands
//
// for WordCraft JSON documents (.json). Other subcommands, options and formats report that they
// aren't ported yet.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/computernut43/wordcraft/internal/doc"
	"github.com/computernut43/wordcraft/internal/engine"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the CLI: it returns the exit status, 0 on success, 1 on any error (printed to stderr
// as "wordcraft-cli: …", as the Rust CLI does).
func run(args []string, stdout, stderr io.Writer) int {
	if err := dispatch(args, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "wordcraft-cli: %v\n", err)
		return 1
	}
	return 0
}

var errNotPorted = errors.New("not available in the Go port yet")

func dispatch(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: wordcraft-cli convert IN OUT | run --file IN [--cmd ID[=JSON]]… [--save OUT] [--print] | commands")
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "convert":
		return convert(rest, stderr)
	case "run":
		return runCommands(rest, stdout, stderr)
	case "commands":
		s, err := engine.New(&doc.Document{Body: []doc.Block{{Para: &doc.Paragraph{Runs: []doc.Run{}}}}})
		if err != nil {
			return err
		}
		for _, c := range s.Commands() {
			fmt.Fprintf(stdout, "%-32s %-36s %-24s %s\n", c.ID, c.Label, c.Shortcut, c.Location)
		}
		return nil
	}
	return fmt.Errorf("%w: the %q subcommand", errNotPorted, cmd)
}

func convert(args []string, stderr io.Writer) error {
	var paths []string
	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			return fmt.Errorf("%w: convert option %s", errNotPorted, a)
		}
		paths = append(paths, a)
	}
	if len(paths) < 2 {
		return errors.New("convert: missing argument (convert IN OUT)")
	}
	s, err := open(paths[0])
	if err != nil {
		return err
	}
	if err := s.Save(paths[1]); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "wrote %s\n", paths[1])
	return nil
}

func open(path string) (*engine.Session, error) {
	d, err := engine.Open(path)
	if err != nil {
		return nil, err
	}
	return engine.New(d)
}

type step struct {
	id     string
	params engine.Params
}

func runCommands(args []string, stdout, stderr io.Writer) error {
	var file, save string
	var steps []step
	print := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		value := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", a)
			}
			i++
			return args[i], nil
		}
		var err error
		switch a {
		case "--file":
			file, err = value()
		case "--save":
			save, err = value()
		case "--print":
			print = true
		case "--cmd":
			var spec string
			if spec, err = value(); err == nil {
				var st step
				st, err = parseStep(spec)
				steps = append(steps, st)
			}
		default:
			err = fmt.Errorf("%w: run option %s", errNotPorted, a)
		}
		if err != nil {
			return err
		}
	}
	if file == "" {
		return fmt.Errorf("%w: run without --file (templates and blank documents)", errNotPorted)
	}
	s, err := open(file)
	if err != nil {
		return err
	}
	for _, st := range steps {
		r, err := s.Run(st.id, st.params)
		if err != nil {
			return fmt.Errorf("%s: %w", st.id, err)
		}
		if print {
			out, err := sortedJSON(r)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "%s: %s\n", st.id, out)
		}
	}
	if save != "" {
		if err := s.Save(save); err != nil {
			return err
		}
		fmt.Fprintf(stderr, "wrote %s\n", save)
	}
	return nil
}

// parseStep reads ID or ID=JSON, as the Rust CLI's --cmd.
func parseStep(spec string) (step, error) {
	id, raw, ok := strings.Cut(spec, "=")
	if !ok {
		return step{id: spec, params: engine.Params{}}, nil
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return step{}, fmt.Errorf("%s: bad JSON params: %w", id, err)
	}
	p, _ := v.(map[string]any)
	return step{id: id, params: engine.Params(p)}, nil
}

// sortedJSON writes v compactly with object keys sorted, as Rust's serde_json Value does.
func sortedJSON(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return "", err
	}
	out, err := json.Marshal(generic)
	return string(out), err
}
