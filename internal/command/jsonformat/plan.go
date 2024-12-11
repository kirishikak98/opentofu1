// Copyright (c) The OpenTofu Authors
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2023 HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package jsonformat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/opentofu/opentofu/internal/command/format"
	"github.com/opentofu/opentofu/internal/command/jsonformat/computed"
	"github.com/opentofu/opentofu/internal/command/jsonformat/computed/renderers"
	"github.com/opentofu/opentofu/internal/command/jsonplan"
	"github.com/opentofu/opentofu/internal/command/jsonprovider"
	"github.com/opentofu/opentofu/internal/command/jsonstate"
	"github.com/opentofu/opentofu/internal/plans"
)

const (
	detectedDrift  string = "drift"
	proposedChange string = "change"
)

type Plan struct {
	PlanFormatVersion  string                     `json:"plan_format_version"`
	OutputChanges      map[string]jsonplan.Change `json:"output_changes"`
	ResourceChanges    []jsonplan.ResourceChange  `json:"resource_changes"`
	ResourceDrift      []jsonplan.ResourceChange  `json:"resource_drift"`
	RelevantAttributes []jsonplan.ResourceAttr    `json:"relevant_attributes"`

	ProviderFormatVersion string                            `json:"provider_format_version"`
	ProviderSchemas       map[string]*jsonprovider.Provider `json:"provider_schemas"`
}

func (plan *Plan) ForgetResource(resourceName string) error {
	for i, change := range plan.ResourceChanges {
		if change.Name == resourceName {
			// Remove the resource change from the slice
			plan.ResourceChanges = append(plan.ResourceChanges[:i], plan.ResourceChanges[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("resource '%s' not found", resourceName)
}

func (plan Plan) getSchema(change jsonplan.ResourceChange) *jsonprovider.Schema {
	switch change.Mode {
	case jsonstate.ManagedResourceMode:
		return plan.ProviderSchemas[change.ProviderName].ResourceSchemas[change.Type]
	case jsonstate.DataResourceMode:
		return plan.ProviderSchemas[change.ProviderName].DataSourceSchemas[change.Type]
	default:
		panic("found unrecognized resource mode: " + change.Mode)
	}
}

func (plan Plan) renderHuman(renderer Renderer, mode plans.Mode, opts ...plans.Quality) {
	checkOpts := func(target plans.Quality) bool {
		for _, opt := range opts {
			if opt == target {
				return true
			}
		}
		return false
	}

	diffs := precomputeDiffs(plan, mode)
	haveRefreshChanges := renderHumanDiffDrift(renderer, diffs, mode)

	willPrintResourceChanges := false
	counts := make(map[plans.Action]int)
	importingCount := 0
	var changes []diff
	for _, diff := range diffs.changes {
		action := jsonplan.UnmarshalActions(diff.change.Change.Actions)
		if action == plans.NoOp && !diff.Moved() && !diff.Importing() {
			// Don’t show anything for NoOp changes.
			continue
		}
		if action == plans.Delete && diff.change.Mode != jsonstate.ManagedResourceMode {
			// Don’t render anything for deleted data sources.
			continue
		}

		changes = append(changes, diff)

		if diff.Importing() {
			importingCount++
		}

		// Don’t count move-only changes
		if action != plans.NoOp {
			willPrintResourceChanges = true
			counts[action]++
		}
	}

	// Precompute the outputs early, so we can make a decision about whether we
	// display the "there are no changes messages".
	outputs := renderHumanDiffOutputs(renderer, diffs.outputs)

	if len(changes) == 0 && len(outputs) == 0 {
		// TODO: Add the no changes message
	}
}
