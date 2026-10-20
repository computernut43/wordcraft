// Package engine is WordCraft's editing engine: the Go port of crates/engine, growing in steps
// (MIGRATION.md §6). Every user-visible action is a command run by id through Session.Run, as in
// Rust, so the CLI, MCP and UI share one entry point.
//
// The port so far covers the commands and cases listed in each command's documentation. Anything
// outside them is refused with a NotPorted error rather than done differently from Rust.
package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/computernut43/wordcraft/internal/doc"
)

// ErrKind classifies command errors (Rust CmdError, plus NotPorted).
type ErrKind int

// Error kinds.
const (
	// Unknown: no command has that id.
	Unknown ErrKind = iota + 1
	// BadParams: the parameters are missing or wrong.
	BadParams
	// Disabled: the command isn't available in this state.
	Disabled
	// Failed: the command ran and failed.
	Failed
	// NotPorted: the command or this case of it isn't in the Go port yet (Go only).
	NotPorted
)

// CmdError is a command's error, worded as Rust's CmdError.
type CmdError struct {
	Kind ErrKind
	Msg  string
}

func (e *CmdError) Error() string {
	switch e.Kind {
	case Unknown:
		return fmt.Sprintf("unknown command `%s`", e.Msg)
	case BadParams:
		return "bad parameters: " + e.Msg
	case Disabled:
		return "not available: " + e.Msg
	case NotPorted:
		return "not available in the Go port yet: " + e.Msg
	}
	return e.Msg
}

func errParams(format string, a ...any) error {
	return &CmdError{Kind: BadParams, Msg: fmt.Sprintf(format, a...)}
}

func errFailed(format string, a ...any) error {
	return &CmdError{Kind: Failed, Msg: fmt.Sprintf(format, a...)}
}

func notPorted(format string, a ...any) error {
	return &CmdError{Kind: NotPorted, Msg: fmt.Sprintf(format, a...)}
}

// asCmdError turns a model error into a command error (Rust: From<DocError> for CmdError).
func asCmdError(err error) error {
	if err == nil {
		return nil
	}
	var ce *CmdError
	if errors.As(err, &ce) {
		return ce
	}
	var de *doc.Error
	if errors.As(err, &de) {
		return &CmdError{Kind: Failed, Msg: de.Error()}
	}
	return &CmdError{Kind: Failed, Msg: err.Error()}
}

// Params are a command's JSON parameters (an object; anything else counts as no parameters).
type Params map[string]any

// Str is a string parameter (Rust p::str).
func (p Params) Str(k string) (string, bool) {
	s, ok := p[k].(string)
	return s, ok
}

// ReqStr is a required string parameter (Rust p::req_str).
func (p Params) ReqStr(k string) (string, error) {
	if s, ok := p.Str(k); ok {
		return s, nil
	}
	return "", errParams("`%s` (string) is required", k)
}

// Bool is a boolean parameter (Rust p::bool: only a JSON boolean counts).
func (p Params) Bool(k string) (bool, bool) {
	b, ok := p[k].(bool)
	return b, ok
}

// U64 is an unsigned integer parameter (Rust p::u64): an exact unsigned integer, or else a
// non-negative finite number, truncated. Params decoded with json.Decoder.UseNumber keep large
// integers exact.
func (p Params) U64(k string) (uint64, bool) {
	var f float64
	switch v := p[k].(type) {
	case json.Number:
		if n, err := strconv.ParseUint(string(v), 10, 64); err == nil {
			return n, true
		}
		x, err := v.Float64()
		if err != nil {
			return 0, false
		}
		f = x
	case float64:
		f = v
	default:
		return 0, false
	}
	switch {
	case !(f >= 0) || math.IsInf(f, 1):
		return 0, false
	case f >= 1.8446744073709552e19:
		return math.MaxUint64, true
	}
	return uint64(f), true
}
