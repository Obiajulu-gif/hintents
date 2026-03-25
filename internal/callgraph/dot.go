// Copyright 2026 Erst Users
// SPDX-License-Identifier: Apache-2.0

package callgraph

import (
	"encoding/base64"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/dotandev/hintents/internal/simulator"
	"github.com/stellar/go-stellar-sdk/xdr"
)

const rootContractID = "ROOT"

type invocationNode struct {
	ContractID   string
	FunctionName string
	Parent       *invocationNode
	Children     []*invocationNode
	SelfUnits    uint64
	SubtreeUnits uint64
}

type edgeKey struct {
	Caller string
	Callee string
}

type edgeStat struct {
	GasCost uint64
	Calls   int
}

type parsedTopics struct {
	Primary  string
	Function string
	IsCall   bool
	IsReturn bool
}

// ExportDOT converts the simulator diagnostic trace into a Graphviz DOT call graph.
func ExportDOT(resp *simulator.SimulationResponse) (string, error) {
	if resp == nil {
		return "", fmt.Errorf("simulation response is nil")
	}
	if len(resp.DiagnosticEvents) == 0 {
		return "", fmt.Errorf("simulation response does not contain diagnostic events")
	}

	root := &invocationNode{
		ContractID:   rootContractID,
		FunctionName: "transaction",
	}
	current := root
	contracts := make(map[string]struct{})

	for _, event := range resp.DiagnosticEvents {
		topics, err := parseTopics(event.Topics)
		if err != nil {
			topics = fallbackTopics(event.Topics)
		}

		units := eventUnits(topics.Primary)

		switch {
		case topics.IsCall:
			childContract := stringValue(event.ContractID)
			if childContract == "" {
				childContract = current.ContractID
			}
			child := &invocationNode{
				ContractID:   childContract,
				FunctionName: topics.Function,
				Parent:       current,
				SelfUnits:    units,
			}
			current.Children = append(current.Children, child)
			current = child
			if child.ContractID != "" && child.ContractID != rootContractID {
				contracts[child.ContractID] = struct{}{}
			}
		case topics.IsReturn:
			current.SelfUnits += units
			current = unwindStack(current, topics.Function)
		default:
			current.SelfUnits += units
			if contractID := stringValue(event.ContractID); contractID != "" && contractID != rootContractID {
				contracts[contractID] = struct{}{}
			}
		}
	}

	totalUnits := computeSubtreeUnits(root)
	if len(contracts) == 0 {
		return "", fmt.Errorf("no contract IDs were found in the diagnostic trace")
	}

	totalGas := estimatedTransactionGas(resp, totalUnits)
	edges := make(map[edgeKey]*edgeStat)
	collectEdges(root, totalUnits, totalGas, edges)

	return renderDOT(contracts, edges), nil
}

func unwindStack(current *invocationNode, functionName string) *invocationNode {
	if current == nil {
		return nil
	}
	if functionName == "" {
		if current.Parent != nil {
			return current.Parent
		}
		return current
	}

	iter := current
	for iter != nil && iter.ContractID != rootContractID {
		if iter.FunctionName == functionName {
			if iter.Parent != nil {
				return iter.Parent
			}
			return iter
		}
		iter = iter.Parent
	}

	if current.Parent != nil {
		return current.Parent
	}
	return current
}

func computeSubtreeUnits(node *invocationNode) uint64 {
	if node == nil {
		return 0
	}
	total := node.SelfUnits
	for _, child := range node.Children {
		total += computeSubtreeUnits(child)
	}
	node.SubtreeUnits = total
	return total
}

func estimatedTransactionGas(resp *simulator.SimulationResponse, fallback uint64) uint64 {
	if resp.BudgetUsage == nil {
		if fallback == 0 {
			return 1
		}
		return fallback
	}

	gas, err := resp.BudgetUsage.ToGasEstimation()
	if err != nil || gas.EstimatedFeeLowerBound <= 0 {
		if fallback == 0 {
			return 1
		}
		return fallback
	}

	return uint64(gas.EstimatedFeeLowerBound)
}

func collectEdges(node *invocationNode, totalUnits, totalGas uint64, edges map[edgeKey]*edgeStat) {
	if node == nil {
		return
	}

	for _, child := range node.Children {
		if node.ContractID != rootContractID && child.ContractID != "" && node.ContractID != "" && child.ContractID != node.ContractID {
			key := edgeKey{Caller: node.ContractID, Callee: child.ContractID}
			if edges[key] == nil {
				edges[key] = &edgeStat{}
			}
			edges[key].Calls++
			edges[key].GasCost += scaleGas(child.SubtreeUnits, totalUnits, totalGas)
		}
		collectEdges(child, totalUnits, totalGas, edges)
	}
}

func scaleGas(edgeUnits, totalUnits, totalGas uint64) uint64 {
	if edgeUnits == 0 {
		return 1
	}
	if totalUnits == 0 || totalGas == 0 {
		return edgeUnits
	}
	scaled := uint64(math.Round((float64(edgeUnits) / float64(totalUnits)) * float64(totalGas)))
	if scaled == 0 {
		return 1
	}
	return scaled
}

func renderDOT(contracts map[string]struct{}, edges map[edgeKey]*edgeStat) string {
	nodeIDs := make([]string, 0, len(contracts))
	for contractID := range contracts {
		nodeIDs = append(nodeIDs, contractID)
	}
	sort.Strings(nodeIDs)

	edgeIDs := make([]edgeKey, 0, len(edges))
	for key := range edges {
		edgeIDs = append(edgeIDs, key)
	}
	sort.Slice(edgeIDs, func(i, j int) bool {
		if edgeIDs[i].Caller != edgeIDs[j].Caller {
			return edgeIDs[i].Caller < edgeIDs[j].Caller
		}
		return edgeIDs[i].Callee < edgeIDs[j].Callee
	})

	var b strings.Builder
	b.WriteString("digraph call_graph {\n")
	b.WriteString("  rankdir=LR;\n")
	b.WriteString("  graph [label=\"Erst Call Graph\", labelloc=t, fontsize=20];\n")
	b.WriteString("  node [shape=box, style=\"rounded,filled\", fillcolor=\"#f8fafc\", color=\"#475569\"];\n")
	b.WriteString("  edge [color=\"#334155\"];\n")

	for _, contractID := range nodeIDs {
		escaped := escapeDOT(contractID)
		fmt.Fprintf(&b, "  \"%s\";\n", escaped)
	}

	for _, key := range edgeIDs {
		stat := edges[key]
		label := fmt.Sprintf("gas=%d calls=%d", stat.GasCost, stat.Calls)
		fmt.Fprintf(
			&b,
			"  \"%s\" -> \"%s\" [label=\"%s\", weight=%d, gas_cost=%d, calls=%d];\n",
			escapeDOT(key.Caller),
			escapeDOT(key.Callee),
			escapeDOT(label),
			stat.GasCost,
			stat.GasCost,
			stat.Calls,
		)
	}

	b.WriteString("}\n")
	return b.String()
}

func parseTopics(rawTopics []string) (parsedTopics, error) {
	var parsed parsedTopics
	if len(rawTopics) == 0 {
		return parsed, nil
	}

	primary, err := decodeTopicSymbol(rawTopics[0])
	if err != nil {
		return parsed, err
	}
	parsed.Primary = primary
	parsed.IsCall = primary == "fn_call"
	parsed.IsReturn = primary == "fn_return"

	if len(rawTopics) > 1 {
		if fn, err := decodeTopicSymbol(rawTopics[1]); err == nil {
			parsed.Function = fn
		}
	}

	return parsed, nil
}

func fallbackTopics(rawTopics []string) parsedTopics {
	var parsed parsedTopics
	if len(rawTopics) == 0 {
		return parsed
	}
	parsed.Primary = strings.ToLower(rawTopics[0])
	parsed.IsCall = strings.Contains(parsed.Primary, "fn_call")
	parsed.IsReturn = strings.Contains(parsed.Primary, "fn_return")
	if len(rawTopics) > 1 {
		parsed.Function = rawTopics[1]
	}
	return parsed
}

func decodeTopicSymbol(value string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}

	var scv xdr.ScVal
	if err := xdr.SafeUnmarshal(data, &scv); err != nil {
		return "", err
	}

	if scv.Type == xdr.ScValTypeScvSymbol && scv.Sym != nil {
		return string(*scv.Sym), nil
	}

	return "", fmt.Errorf("topic is not a symbol")
}

func eventUnits(primaryTopic string) uint64 {
	switch strings.ToLower(primaryTopic) {
	case "storage_write":
		return 3
	case "require_auth", "auth":
		return 2
	default:
		return 1
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func escapeDOT(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return value
}
