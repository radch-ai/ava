package mcp

import (
	"io"
	"reflect"
	"unsafe"

	"github.com/mark3labs/mcp-go/client/transport"
)

// drainStderr discards the stderr pipe mcp-go creates for an stdio
// transport and never reads (v0.43.0, client/transport/stdio.go:186-198):
// a server that writes more than the OS pipe buffer (~64 KiB) to stderr
// blocks forever on that write, and every later call dies on its deadline.
//
// Upstream fixed exactly this in v1.0.0 (#963, "drain stdio subprocess
// stderr to avoid pipe-full deadlock"), but eino-ext/components/tool/mcp
// v0.0.9 pins mcp-go v0.43.0 while our seam hands it transports and
// mcpsdk types — so the fix cannot be taken until eino-ext ships a v1.x
// adapter. The read side is unexported, hence the reflection: this file
// is version-pinned, and TestStdioStderrFloodDoesNotDeadlock is the
// tripwire that fails loudly if a dependency bump renames the field.
//
// Fails open: a different transport, or a field that is gone, leaves the
// dial exactly as it was before this function existed.
func drainStderr(tr transport.Interface) bool {
	stdio, ok := tr.(*transport.Stdio)
	if !ok {
		return false
	}
	field := reflect.ValueOf(stdio).Elem().FieldByName("stderr")
	if !field.IsValid() || field.Kind() != reflect.Interface || field.IsNil() {
		return false
	}
	// The field is unexported: re-address it at its own type so Interface()
	// is allowed. Safe by construction — the value was obtained through an
	// addressable, still-live struct pointer.
	//nolint:gosec // reading mcp-go's own pipe; see the doc comment.
	pipe, ok := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).
		Elem().Interface().(io.ReadCloser)
	if !ok {
		return false
	}
	go func() { _, _ = io.Copy(io.Discard, pipe) }()
	return true
}
