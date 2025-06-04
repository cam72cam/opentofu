// Copyright (c) The OpenTofu Authors
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2023 HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tofu

//go:generate go run golang.org/x/tools/cmd/stringer -type=walkOperation graph_walk_operation.go

// WalkOperation is an enum which tells the walkContext what to do.
type WalkOperation byte

const (
	walkInvalid WalkOperation = iota
	walkApply
	walkPlan
	walkPlanDestroy
	walkValidate
	walkDestroy
	walkImport
	walkEval // used just to prepare EvalContext for expression evaluation, with no other actions
)
