package main

import (
	"fmt"
	"math"
)

const (
	algorithmVersion        = "pagerank-v1"
	canonicalizationVersion = 1
	pageRankDamping         = 0.85
	pageRankTolerance       = 1e-12
	pageRankMaxIterations   = 1000
)

type pageRankResult struct {
	Ranks      []float64
	Iterations int
	Residual   float64
	Sum        float64
}

func calculatePageRank(input graph) (pageRankResult, error) {
	return calculatePageRankWithParameters(
		input,
		pageRankDamping,
		pageRankTolerance,
		pageRankMaxIterations,
	)
}

func calculatePageRankWithParameters(input graph, damping, tolerance float64, maxIterations int) (pageRankResult, error) {
	nodeCount := len(input.Nodes)
	if nodeCount == 0 || len(input.Outgoing) != nodeCount {
		return pageRankResult{}, fmt.Errorf("pagerank: invalid empty or inconsistent graph")
	}
	if damping <= 0 || damping >= 1 || tolerance <= 0 || maxIterations <= 0 {
		return pageRankResult{}, fmt.Errorf("pagerank: invalid algorithm parameters")
	}

	ranks := make([]float64, nodeCount)
	for index := range ranks {
		ranks[index] = 1 / float64(nodeCount)
	}

	for iteration := 1; iteration <= maxIterations; iteration++ {
		next := pageRankStep(input, ranks, damping)
		residual := l1Distance(ranks, next)
		if err := validateRanks(next); err != nil {
			return pageRankResult{}, err
		}
		ranks = next

		if residual <= tolerance {
			stationaryResidual := l1Distance(ranks, pageRankStep(input, ranks, damping))
			sum := sumRanks(ranks)
			if math.Abs(sum-1) > pageRankTolerance {
				return pageRankResult{}, fmt.Errorf("pagerank: rank sum %.17g is not normalized", sum)
			}
			return pageRankResult{
				Ranks:      ranks,
				Iterations: iteration,
				Residual:   stationaryResidual,
				Sum:        sum,
			}, nil
		}
	}

	return pageRankResult{}, fmt.Errorf("pagerank: did not converge after %d iterations", maxIterations)
}

func pageRankStep(input graph, ranks []float64, damping float64) []float64 {
	nodeCount := len(input.Nodes)
	danglingMass := 0.0
	for sourceIndex, targets := range input.Outgoing {
		if len(targets) == 0 {
			danglingMass += ranks[sourceIndex]
		}
	}

	base := (1-damping)/float64(nodeCount) + damping*danglingMass/float64(nodeCount)
	next := make([]float64, nodeCount)
	for index := range next {
		next[index] = base
	}
	for sourceIndex, targets := range input.Outgoing {
		if len(targets) == 0 {
			continue
		}
		contribution := damping * ranks[sourceIndex] / float64(len(targets))
		for _, targetIndex := range targets {
			next[targetIndex] += contribution
		}
	}
	return next
}

func validateRanks(ranks []float64) error {
	for index, rank := range ranks {
		if math.IsNaN(rank) || math.IsInf(rank, 0) || rank < 0 || rank > 1 {
			return fmt.Errorf("pagerank: invalid rank at index %d", index)
		}
	}
	return nil
}

func l1Distance(left, right []float64) float64 {
	total := 0.0
	for index := range left {
		total += math.Abs(left[index] - right[index])
	}
	return total
}

func sumRanks(ranks []float64) float64 {
	total := 0.0
	compensation := 0.0
	for _, rank := range ranks {
		adjusted := rank - compensation
		next := total + adjusted
		compensation = (next - total) - adjusted
		total = next
	}
	return total
}
