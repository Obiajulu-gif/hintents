// Copyright 2026 Erst Users
// SPDX-License-Identifier: Apache-2.0

package callgraph

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/dotandev/hintents/internal/simulator"
	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/stretchr/testify/require"
)

func TestExportDOT_CrossContractEdgesAreWeighted(t *testing.T) {
	contractA := contractID(0xAA)
	contractB := contractID(0xBB)
	contractC := contractID(0xCC)

	resp := &simulator.SimulationResponse{
		DiagnosticEvents: []simulator.DiagnosticEvent{
			callEvent(t, contractA, "entry"),
			callEvent(t, contractB, "transfer"),
			regularEvent(t, contractB, "storage_write"),
			returnEvent(t, contractB, "transfer"),
			callEvent(t, contractC, "settle"),
			regularEvent(t, contractC, "require_auth"),
			returnEvent(t, contractC, "settle"),
			returnEvent(t, contractA, "entry"),
		},
		BudgetUsage: &simulator.BudgetUsage{
			CPUInstructions: 120_000,
			MemoryBytes:     128 * 1024,
			CPULimit:        1_000_000,
			MemoryLimit:     10 * 1024 * 1024,
		},
	}

	dot, err := ExportDOT(resp)
	require.NoError(t, err)
	require.Contains(t, dot, "\""+contractA+"\" -> \""+contractB+"\"")
	require.Contains(t, dot, "\""+contractA+"\" -> \""+contractC+"\"")
	require.Contains(t, dot, "gas_cost=")
	require.Contains(t, dot, "calls=1")
}

func TestExportDOT_SkipsSameContractEdges(t *testing.T) {
	contractA := contractID(0xAA)
	contractB := contractID(0xBB)

	resp := &simulator.SimulationResponse{
		DiagnosticEvents: []simulator.DiagnosticEvent{
			callEvent(t, contractA, "entry"),
			callEvent(t, contractA, "internal_helper"),
			returnEvent(t, contractA, "internal_helper"),
			callEvent(t, contractB, "transfer"),
			returnEvent(t, contractB, "transfer"),
			returnEvent(t, contractA, "entry"),
		},
	}

	dot, err := ExportDOT(resp)
	require.NoError(t, err)
	require.NotContains(t, dot, "\""+contractA+"\" -> \""+contractA+"\"")
	require.Contains(t, dot, "\""+contractA+"\" -> \""+contractB+"\"")
}

func TestExportDOT_RequiresDiagnosticEvents(t *testing.T) {
	_, err := ExportDOT(&simulator.SimulationResponse{})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "diagnostic events"))
}

func callEvent(t *testing.T, contractID string, fn string) simulator.DiagnosticEvent {
	t.Helper()
	return simulator.DiagnosticEvent{
		EventType:  "diagnostic",
		ContractID: &contractID,
		Topics: []string{
			encodeSymbol(t, "fn_call"),
			encodeSymbol(t, fn),
		},
		Data: encodeVoid(t),
	}
}

func returnEvent(t *testing.T, contractID string, fn string) simulator.DiagnosticEvent {
	t.Helper()
	return simulator.DiagnosticEvent{
		EventType:  "diagnostic",
		ContractID: &contractID,
		Topics: []string{
			encodeSymbol(t, "fn_return"),
			encodeSymbol(t, fn),
		},
		Data: encodeVoid(t),
	}
}

func regularEvent(t *testing.T, contractID string, topic string) simulator.DiagnosticEvent {
	t.Helper()
	return simulator.DiagnosticEvent{
		EventType:  "diagnostic",
		ContractID: &contractID,
		Topics: []string{
			encodeSymbol(t, topic),
		},
		Data: encodeVoid(t),
	}
}

func encodeSymbol(t *testing.T, value string) string {
	t.Helper()
	sym := xdr.ScSymbol(value)
	scv := xdr.ScVal{
		Type: xdr.ScValTypeScvSymbol,
		Sym:  &sym,
	}
	raw, err := scv.MarshalBinary()
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(raw)
}

func encodeVoid(t *testing.T) string {
	t.Helper()
	scv := xdr.ScVal{Type: xdr.ScValTypeScvVoid}
	raw, err := scv.MarshalBinary()
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(raw)
}

func contractID(seed byte) string {
	return strings.Repeat(fmt.Sprintf("%02x", seed), 32)
}
