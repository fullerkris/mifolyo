package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

type outlinkDocument struct {
	ID    string   `bson:"_id"`
	Links []string `bson:"links"`
}

type graphStats struct {
	NodeCount               int `json:"node_count"`
	InternalEdgeCount       int `json:"internal_edge_count"`
	FilteredTargetCount     int `json:"filtered_target_count"`
	DuplicateEdgeCount      int `json:"duplicate_edge_count"`
	IgnoredSourceCount      int `json:"ignored_source_count"`
	MissingOutlinksCount    int `json:"missing_outlinks_count"`
	DanglingNodeCount       int `json:"dangling_node_count"`
	RawOutlinkDocumentCount int `json:"raw_outlink_document_count"`
}

type graph struct {
	Nodes    []string
	Outgoing [][]int
	SHA256   string
	Stats    graphStats
}

type graphHashInput struct {
	Nodes []string    `json:"nodes"`
	Edges [][2]string `json:"edges"`
}

type redactedOperationalError struct {
	category string
	cause    error
}

func (err *redactedOperationalError) Error() string {
	return err.category
}

func (err *redactedOperationalError) Unwrap() error {
	return err.cause
}

func wrapOperationalError(category string, cause error) error {
	return &redactedOperationalError{category: category, cause: cause}
}

func valueReference(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:8])
}

func buildGraph(nodeIDs []string, outlinkDocuments []outlinkDocument) (graph, error) {
	if len(nodeIDs) == 0 {
		return graph{}, fmt.Errorf("pagerank: metadata node set is empty")
	}

	nodes := append([]string(nil), nodeIDs...)
	sort.Strings(nodes)
	nodeIndex := make(map[string]int, len(nodes))
	for index, node := range nodes {
		if err := validateURLIdentity(node); err != nil {
			return graph{}, fmt.Errorf("pagerank: invalid metadata URL ref=%s: %w", valueReference(node), err)
		}
		if index > 0 && nodes[index-1] == node {
			return graph{}, fmt.Errorf("pagerank: duplicate metadata URL ref=%s", valueReference(node))
		}
		nodeIndex[node] = index
	}

	outgoingSets := make([]map[int]struct{}, len(nodes))
	seenSources := make(map[string]struct{}, len(outlinkDocuments))
	stats := graphStats{
		NodeCount:               len(nodes),
		RawOutlinkDocumentCount: len(outlinkDocuments),
	}

	for _, document := range outlinkDocuments {
		if err := validateURLIdentity(document.ID); err != nil {
			return graph{}, fmt.Errorf("pagerank: invalid outlink source ref=%s: %w", valueReference(document.ID), err)
		}
		if _, exists := seenSources[document.ID]; exists {
			return graph{}, fmt.Errorf("pagerank: duplicate outlink source ref=%s", valueReference(document.ID))
		}
		seenSources[document.ID] = struct{}{}

		sourceIndex, sourceIsRankable := nodeIndex[document.ID]
		if !sourceIsRankable {
			stats.IgnoredSourceCount++
		}

		seenTargets := make(map[string]struct{}, len(document.Links))
		for _, target := range document.Links {
			if err := validateURLIdentity(target); err != nil {
				return graph{}, fmt.Errorf(
					"pagerank: invalid outlink target source_ref=%s target_ref=%s: %w",
					valueReference(document.ID),
					valueReference(target),
					err,
				)
			}
			if _, exists := seenTargets[target]; exists {
				stats.DuplicateEdgeCount++
				continue
			}
			seenTargets[target] = struct{}{}

			if !sourceIsRankable {
				continue
			}
			targetIndex, targetIsRankable := nodeIndex[target]
			if !targetIsRankable {
				stats.FilteredTargetCount++
				continue
			}
			if outgoingSets[sourceIndex] == nil {
				outgoingSets[sourceIndex] = make(map[int]struct{})
			}
			outgoingSets[sourceIndex][targetIndex] = struct{}{}
		}
	}

	outgoing := make([][]int, len(nodes))
	edges := make([][2]string, 0)
	for sourceIndex, targets := range outgoingSets {
		if _, exists := seenSources[nodes[sourceIndex]]; !exists {
			stats.MissingOutlinksCount++
		}
		if len(targets) == 0 {
			stats.DanglingNodeCount++
			continue
		}

		outgoing[sourceIndex] = make([]int, 0, len(targets))
		for targetIndex := range targets {
			outgoing[sourceIndex] = append(outgoing[sourceIndex], targetIndex)
		}
		sort.Ints(outgoing[sourceIndex])
		for _, targetIndex := range outgoing[sourceIndex] {
			edges = append(edges, [2]string{nodes[sourceIndex], nodes[targetIndex]})
		}
	}
	stats.InternalEdgeCount = len(edges)

	hashInput, err := json.Marshal(graphHashInput{Nodes: nodes, Edges: edges})
	if err != nil {
		return graph{}, wrapOperationalError("pagerank: encode graph hash input", err)
	}
	digest := sha256.Sum256(hashInput)

	return graph{
		Nodes:    nodes,
		Outgoing: outgoing,
		SHA256:   hex.EncodeToString(digest[:]),
		Stats:    stats,
	}, nil
}
