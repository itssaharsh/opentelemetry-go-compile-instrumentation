// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"bytes"
	"log"
	"slices"

	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationKey = "logs/log"
	traceIDKey         = "trace_id"
	spanIDKey          = "span_id"
)

// Byte forms of the keys above. The hook works on the log line as a []byte, so
// keeping these as bytes avoids turning the line into a string to search it.
var (
	traceIDKeyBytes = []byte(traceIDKey)
	spanIDKeyBytes  = []byte(spanIDKey)
)

type logEnabler struct{}

func (l logEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = logEnabler{}

func BeforeLogOutput(
	ictx hook.HookContext,
	logger *log.Logger,
	pc uintptr,
	calldepth int,
	appendOutput func([]byte) []byte,
) {
	if !enabler.Enable() {
		return
	}

	newAppendOutput := func(b []byte) []byte {
		b = appendOutput(b)
		if len(b) == 0 {
			return b
		}

		if bytes.Contains(b, traceIDKeyBytes) {
			return b
		}

		traceID, spanID := runtime.GetTraceAndSpanID()
		if traceID == "" {
			return b
		}

		// Trace and span ids are short and of a known width, so the suffix is
		// assembled in a small array here instead of a strings.Builder. A
		// longer id than expected still works, append just grows it.
		var buf [96]byte
		suffix := append(buf[:0], ' ')
		suffix = append(suffix, traceIDKeyBytes...)
		suffix = append(suffix, '=')
		suffix = append(suffix, traceID...)

		if spanID != "" {
			suffix = append(suffix, ' ')
			suffix = append(suffix, spanIDKeyBytes...)
			suffix = append(suffix, '=')
			suffix = append(suffix, spanID...)
		}

		// Keep the ids on the log line itself, ahead of any line ending.
		idx := len(b)
		for idx > 0 && (b[idx-1] == '\n' || b[idx-1] == '\r') {
			idx--
		}

		return slices.Insert(b, idx, suffix...)
	}

	ictx.SetParam(3, newAppendOutput)
}
