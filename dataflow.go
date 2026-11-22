package spl

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
)

// QueryDataflow records ordered field environments without evaluating SPL.
type QueryDataflow struct {
	Complete      bool                 `json:"complete"`
	OutputScopeID string               `json:"output_scope_id,omitempty"`
	Scopes        []FieldScope         `json:"scopes,omitempty"`
	Stages        []DataflowStage      `json:"stages,omitempty"`
	Bindings      []FieldBinding       `json:"bindings,omitempty"`
	Diagnostics   []DataflowDiagnostic `json:"diagnostics,omitempty"`
}

type FieldScope struct {
	ID            string   `json:"id"`
	Pipeline      int      `json:"pipeline"`
	Mode          string   `json:"mode"`
	ParentID      string   `json:"parent_id,omitempty"`
	Source        string   `json:"source,omitempty"`
	BindingIDs    []string `json:"binding_ids,omitempty"`
	RemovedFields []string `json:"removed_fields,omitempty"`
}

type ScopeReference struct {
	QueryPath string `json:"query_path,omitempty"`
	ScopeID   string `json:"scope_id"`
}

type DataflowStage struct {
	ID            string           `json:"id"`
	Pipeline      int              `json:"pipeline"`
	PipeStage     int              `json:"pipe_stage"`
	Command       string           `json:"command"`
	Source        string           `json:"source"`
	Effect        string           `json:"effect"`
	Inputs        []ScopeReference `json:"inputs,omitempty"`
	OutputScopeID string           `json:"output_scope_id"`
	BindingIDs    []string         `json:"binding_ids,omitempty"`
}

type FieldBinding struct {
	ID           string `json:"id"`
	StageID      string `json:"stage_id"`
	Ordinal      int    `json:"ordinal"`
	Field        string `json:"field"`
	Kind         string `json:"kind"`
	Expression   string `json:"expression"`
	InputScopeID string `json:"input_scope_id"`
}

type FieldBindingRef struct {
	ScopeID   string `json:"scope_id"`
	Kind      string `json:"kind"`
	BindingID string `json:"binding_id,omitempty"`
}

// pendingFieldBinding is bounded inline identity retained only until the
// extraction has finished grouping. It never appears in a returned result.
type pendingFieldBinding struct {
	value FieldBindingRef
	valid bool
}

type DataflowDiagnostic struct {
	Code    string `json:"code"`
	StageID string `json:"stage_id,omitempty"`
	Message string `json:"message"`
}

type dataflowOwnerFrame struct {
	OwnerID        int
	Pipeline       int
	InputScopeID   string
	CurrentScopeID string
	Selected       bool
	ExternalChild  bool
}

// The outer listener keeps its own owner; separately extracted children get
// independent listeners. The table makes child suppression structural rather
// than dependent on a command's text or its legacy PipeStage counter.
func (e *conditionExtractor) enterDataflowOwner(ctx antlr.ParserRuleContext) {
	e.budget.step()
	id := len(e.ownerByContext) + 1
	e.ownerByContext[ctx] = id
	e.dataflowOwners = append(e.dataflowOwners, dataflowOwnerFrame{OwnerID: id, ExternalChild: true})
}

func (e *conditionExtractor) leaveDataflowOwner() {
	if len(e.dataflowOwners) > 1 {
		e.dataflowOwners = e.dataflowOwners[:len(e.dataflowOwners)-1]
	}
}

func (e *conditionExtractor) ownsDataflow(ctx antlr.ParserRuleContext) bool {
	// Direct listener unit fixtures have no owner table; they are local owners.
	if len(e.dataflowOwners) == 0 {
		return true
	}
	if e.dataflowOwners[len(e.dataflowOwners)-1].ExternalChild {
		return false
	}
	for node := antlr.Tree(ctx); node != nil; node = node.GetParent() {
		e.budget.step()
		if parent, ok := node.(antlr.ParserRuleContext); ok {
			if owner, exists := e.ownerByContext[parent]; exists {
				return owner == e.dataflowOwners[0].OwnerID
			}
		}
	}
	return true
}

type splFieldSelector struct {
	name   string
	source string
}

type splDataflowBuilder struct {
	flow                   *QueryDataflow
	budget                 *parseBudget
	extractor              *conditionExtractor
	current                string
	scopeIndex             map[string]int
	bindings               map[string]int
	assignmentSources      map[string]string
	argumentlessAggregates map[string]bool
	stages                 map[antlr.ParserRuleContext]int
	warnings               []string
	sourceFailure          bool
}

func newSPLDataflowBuilder(e *conditionExtractor) *splDataflowBuilder {
	e.budget.reserveOutput(1)
	b := &splDataflowBuilder{
		flow: &QueryDataflow{Complete: true}, budget: e.budget, extractor: e,
		scopeIndex: make(map[string]int), bindings: make(map[string]int),
		assignmentSources:      make(map[string]string),
		argumentlessAggregates: make(map[string]bool),
		stages:                 make(map[antlr.ParserRuleContext]int),
	}
	b.current = b.scope("input", "", nil, nil)
	e.dataflowOwners[0].CurrentScopeID = b.current
	return b
}

func (b *splDataflowBuilder) scope(mode, parent string, bindings, removed []string) string {
	b.budget.step()
	b.budget.reserveOutput(1 + len(bindings) + len(removed))
	id := "s" + strconv.Itoa(len(b.flow.Scopes))
	b.scopeIndex[id] = len(b.flow.Scopes)
	b.flow.Scopes = append(b.flow.Scopes, FieldScope{
		ID: id, Mode: mode, ParentID: parent,
		BindingIDs: append([]string(nil), bindings...), RemovedFields: append([]string(nil), removed...),
	})
	return id
}

func (b *splDataflowBuilder) binding(stage *DataflowStage, field, kind, expression, input string) string {
	b.budget.step()
	b.budget.input(expression)
	b.budget.reserveOutput(2)
	id := "b" + strconv.Itoa(len(b.flow.Bindings))
	b.bindings[id] = len(b.flow.Bindings)
	b.flow.Bindings = append(b.flow.Bindings, FieldBinding{
		ID: id, StageID: stage.ID, Ordinal: len(stage.BindingIDs), Field: field,
		Kind: kind, Expression: expression, InputScopeID: input,
	})
	stage.BindingIDs = append(stage.BindingIDs, id)
	return id
}

func (b *splDataflowBuilder) diagnostic(code, stageID, message string) {
	b.budget.step()
	b.budget.reserveOutput(2)
	b.flow.Complete = false
	b.flow.Diagnostics = append(b.flow.Diagnostics, DataflowDiagnostic{Code: code, StageID: stageID, Message: message})
	b.warnings = append(b.warnings, "dataflow:"+code+": "+message)
}

func (b *splDataflowBuilder) lookup(scopeID, name string) FieldBindingRef {
	ref := FieldBindingRef{ScopeID: scopeID, Kind: "unknown"}
	for id := scopeID; id != ""; {
		b.budget.step()
		index, ok := b.scopeIndex[id]
		if !ok {
			return ref
		}
		scope := b.flow.Scopes[index]
		for i := len(scope.BindingIDs) - 1; i >= 0; i-- {
			b.budget.step()
			binding := b.flow.Bindings[b.bindings[scope.BindingIDs[i]]]
			if binding.Field == name {
				ref.Kind, ref.BindingID = "binding", binding.ID
				return ref
			}
		}
		if scope.Mode == "closed" {
			ref.Kind = "absent"
			return ref
		}
		if scope.Mode == "opaque" {
			return ref
		}
		for _, removed := range scope.RemovedFields {
			b.budget.step()
			if removed == name {
				ref.Kind = "absent"
				return ref
			}
		}
		if scope.Mode == "input" {
			ref.Kind = "input"
			return ref
		}
		id = scope.ParentID
	}
	return ref
}

func (b *splDataflowBuilder) annotateOperand(operand *Operand, scope string, stage *DataflowStage, report bool) {
	if operand == nil {
		return
	}
	b.budget.step()
	if operand.Kind == OperandField {
		operand.pendingFieldBinding = pendingFieldBinding{value: b.lookup(scope, operand.Value), valid: true}
		if report {
			b.referenceDiagnostic(operand.pendingFieldBinding.value, stage, operand.Value)
		}
	}
	for i := range operand.Items {
		b.annotateOperand(&operand.Items[i], scope, stage, report)
	}
}

func (b *splDataflowBuilder) referenceDiagnostic(ref FieldBindingRef, stage *DataflowStage, field string) {
	switch ref.Kind {
	case "absent":
		b.diagnostic("field_not_in_scope", stage.ID, "field "+field+" is absent from scope "+ref.ScopeID)
	case "unknown":
		b.diagnostic("unresolved_field_binding", stage.ID, "field "+field+" cannot resolve through scope "+ref.ScopeID)
	}
}

// Follow only immutable single-name projections. The graph binding owns the
// occurrence's history; the listener's final compatibility map cannot answer
// a read after an intervening table or a later rewrite of the same name.
func (b *splDataflowBuilder) legacyProvenance(ref FieldBindingRef) (computed bool, source string, sourceKnown bool) {
	for hops := 0; hops <= len(b.flow.Bindings) && ref.Kind == "binding"; hops++ {
		b.budget.step()
		index, ok := b.bindings[ref.BindingID]
		if !ok {
			return false, "", false
		}
		binding := b.flow.Bindings[index]
		switch binding.Kind {
		case "assignment":
			hint := b.assignmentSources[binding.ID]
			return true, hint, hint != ""
		case "aggregate":
			for _, aggregation := range b.extractor.aggregations {
				b.budget.step()
				if aggregation.BindingID == binding.ID {
					if aggregation.Field != "" {
						return true, aggregation.Field, true
					}
					// Parser-owned argument and alias syntax decides whether
					// an empty source conclusively replaces an older hint.
					// Named outputs retain their listener function marker.
					return true, "", b.argumentlessAggregates[binding.ID]
				}
			}
			return true, "", false
		case "alias":
			previous := b.lookup(binding.InputScopeID, binding.Expression)
			if previous.Kind != "binding" {
				return true, binding.Expression, true
			}
			ref = previous
		case "passthrough", "group":
			ref = b.lookup(binding.InputScopeID, binding.Field)
		default:
			return false, "", true
		}
	}
	return false, "", true
}

func (b *splDataflowBuilder) annotateStage(stageCtx antlr.ParserRuleContext, stage *DataflowStage, input string) {
	var walk func(antlr.Tree)
	walk = func(node antlr.Tree) {
		b.budget.step()
		if node == nil {
			return
		}
		if _, child := node.(ISubsearchContext); child {
			return
		}
		if sourced, ok := b.extractor.conditionSources[node]; ok {
			if sourced.scalar != nil {
				sourced.scalar.StageID, sourced.scalar.InputScopeID = stage.ID, input
			}
			for i := sourced.first; i < sourced.first+sourced.count && i < len(b.extractor.conditions); i++ {
				condition := &b.extractor.conditions[i]
				condition.StageID, condition.InputScopeID = stage.ID, input
				condition.pendingFieldBinding = pendingFieldBinding{value: b.lookup(input, condition.Field), valid: true}
				b.referenceDiagnostic(condition.pendingFieldBinding.value, stage, condition.Field)
				b.annotateOperand(condition.LHS, input, stage, false)
				b.annotateOperand(condition.RHS, input, stage, true)
				switch condition.pendingFieldBinding.value.Kind {
				case "binding":
					computed, source, sourceKnown := b.legacyProvenance(condition.pendingFieldBinding.value)
					condition.IsComputed = computed
					if sourceKnown || source != "" || !computed {
						condition.SourceField = source
					}
				case "input", "absent":
					condition.IsComputed, condition.SourceField = false, ""
					// Opaque scopes cannot refute legacy command-specific hints.
				}
			}
		}
		for i := 0; i < node.GetChildCount(); i++ {
			walk(node.GetChild(i))
		}
	}
	walk(stageCtx)
}

func splFieldName(ctx IFieldNameContext) string {
	if ctx == nil {
		return ""
	}
	return trimMatchingQuotes(ctx.GetText())
}

// These accepted selectors do not identify one stable output column. Their
// authored spelling remains on the stage, while its output scope is opaque.
func unresolvedSPLFieldName(name string) bool {
	return name == "" || strings.ContainsAny(name, "*?") ||
		strings.Contains(name, "<<") || strings.Contains(name, ">>")
}

// The parser's expression text has already omitted SPL backtick comments.
// Capture only the first legacy source hint when the assignment is created:
// later conditions must not rescan authored text or the final computed map.
func firstSPLFieldHint(text string, budget *parseBudget) string {
	var current strings.Builder
	var quote byte
	for i := 0; i <= len(text); i++ {
		budget.step()
		var ch byte
		if i < len(text) {
			ch = text[i]
		}
		if quote != 0 {
			if ch == quote && (i == 0 || text[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		ident := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '_'
		if ident {
			budget.step() // bound candidate growth as well as the character walk
			current.WriteByte(ch)
			continue
		}
		if current.Len() == 0 {
			continue
		}
		candidate := current.String()
		current.Reset()
		budget.step() // candidate classification
		if isReservedWord(candidate) || startsWithDigit(candidate) {
			continue
		}
		function := false
		for j := i; j < len(text) && (text[j] == ' ' || text[j] == '\t' || text[j] == '('); j++ {
			budget.step() // bounded function-name lookahead
			if text[j] == '(' {
				function = true
				break
			}
		}
		if !function {
			return candidate
		}
	}
	return ""
}

func (b *splDataflowBuilder) fieldSelectors(ctx IFieldListContext) []splFieldSelector {
	if ctx == nil {
		return nil
	}
	var fields []splFieldSelector
	for _, item := range ctx.AllFieldOrQuoted() {
		b.budget.step()
		name := ""
		if item.FieldName() != nil {
			name = splFieldName(item.FieldName())
		}
		if item.QUOTED_STRING() != nil {
			name = decodeSPLQuotedOperand(trimMatchingQuotes(item.QUOTED_STRING().GetText()))
		}
		if name != "" {
			fields = append(fields, splFieldSelector{name: name, source: b.extractor.sourceOf(item)})
		}
	}
	return fields
}

func (b *splDataflowBuilder) fieldList(ctx IFieldListContext) []string {
	selectors := b.fieldSelectors(ctx)
	fields := make([]string, 0, len(selectors))
	for _, selector := range selectors {
		b.budget.step()
		fields = append(fields, selector.name)
	}
	return fields
}

func (b *splDataflowBuilder) build(tree antlr.Tree) (*QueryDataflow, []string) {
	query, ok := tree.(IQueryContext)
	if !ok {
		return b.flow, nil
	}
	pipes := query.AllPIPE()
	pipeCursor, previousPipe := 0, -1
	for _, stageRule := range query.AllPipelineStage() {
		b.budget.step()
		ctx := stageRule.(*PipelineStageContext)
		input := b.current
		for pipeCursor < len(pipes) && pipes[pipeCursor].GetSymbol().GetStart() < ctx.GetStart().GetStart() {
			b.budget.step()
			previousPipe = pipes[pipeCursor].GetSymbol().GetStop()
			pipeCursor++
		}
		nextPipe := -1
		if pipeCursor < len(pipes) {
			nextPipe = pipes[pipeCursor].GetSymbol().GetStart()
		}
		source := b.extractor.sourceMap.stageSource(previousPipe, nextPipe)
		if source == "" {
			b.diagnostic("dataflow_source_unavailable", "", "stage source unavailable")
			return nil, b.warnings
		}
		b.budget.input(source)
		b.budget.reserveOutput(2)
		stage := DataflowStage{
			ID: "t" + strconv.Itoa(len(b.flow.Stages)), Pipeline: 0,
			PipeStage: b.extractor.stageNumbers[ctx], Source: source,
			Inputs: []ScopeReference{{ScopeID: input}},
		}
		b.stageCommand(ctx, &stage)
		b.stageEffect(ctx, &stage, input)
		if b.sourceFailure {
			return nil, b.warnings
		}
		if stage.Command != "join" && stage.Command != "append" {
			b.addStandaloneChildren(ctx, &stage, input)
			if b.sourceFailure {
				return nil, b.warnings
			}
		}
		if stage.OutputScopeID == "" {
			stage.OutputScopeID = b.scope("inherit", input, nil, nil)
		}
		b.stages[ctx] = len(b.flow.Stages)
		b.annotateStage(ctx, &stage, input)
		b.flow.Stages = append(b.flow.Stages, stage)
		b.current = stage.OutputScopeID
		b.extractor.dataflowOwners[0].InputScopeID = input
		b.extractor.dataflowOwners[0].CurrentScopeID = b.current
	}
	b.flow.OutputScopeID = b.current
	return b.flow, b.warnings
}

func (b *splDataflowBuilder) addStandaloneChildren(ctx *PipelineStageContext, stage *DataflowStage, input string) {
	var walk func(antlr.Tree)
	walk = func(node antlr.Tree) {
		b.budget.step()
		if child, ok := node.(*SubsearchContext); ok {
			path := b.extractor.canonicalChildren[child]
			if !strings.HasPrefix(path, "subsearches/") {
				return
			}
			index, present := b.extractor.subsearchIndices[child]
			if !present || index >= len(b.extractor.subsearches) {
				b.sourceFailure = true
				return
			}
			result := b.extractor.subsearches[index]
			if result == nil || result.Dataflow == nil {
				b.sourceFailure = true
				return
			}
			if stage.Effect != "opaque" {
				b.opaque(stage, input)
			}
			b.budget.reserveOutput(1)
			stage.Inputs = append(stage.Inputs, ScopeReference{QueryPath: path, ScopeID: result.Dataflow.OutputScopeID})
			return
		}
		for i := 0; i < node.GetChildCount(); i++ {
			walk(node.GetChild(i))
		}
	}
	walk(ctx)
}

func (b *splDataflowBuilder) stageCommand(ctx *PipelineStageContext, stage *DataflowStage) {
	switch {
	case ctx.SearchCommand() != nil:
		stage.Command = "search"
	case ctx.WhereCommand() != nil:
		stage.Command = "where"
	case ctx.EvalCommand() != nil:
		stage.Command = "eval"
	case ctx.StatsCommand() != nil:
		stage.Command = "stats"
	case ctx.EventstatsCommand() != nil:
		stage.Command = "eventstats"
	case ctx.StreamstatsCommand() != nil:
		stage.Command = "streamstats"
	case ctx.TableCommand() != nil:
		stage.Command = "table"
	case ctx.FieldsCommand() != nil:
		stage.Command = "fields"
	case ctx.RenameCommand() != nil:
		stage.Command = "rename"
	case ctx.JoinCommand() != nil:
		stage.Command = "join"
	case ctx.AppendCommand() != nil:
		stage.Command = "append"
	default:
		stage.Command = strings.ToLower(ctx.GetStart().GetText())
	}
}

func (b *splDataflowBuilder) stageEffect(ctx *PipelineStageContext, stage *DataflowStage, input string) {
	switch {
	case ctx.SearchCommand() != nil:
		if searchHasIndexTimeModifier(ctx.SearchCommand(), ctx.SearchCommand(), b.budget) {
			b.opaque(stage, input)
			return
		}
		stage.Effect = "filter"
	case ctx.WhereCommand() != nil:
		stage.Effect = "filter"
	case ctx.EvalCommand() != nil:
		stage.Effect = "assign"
		for _, assignment := range ctx.EvalCommand().AllEvalAssignment() {
			b.budget.step()
			name := splFieldName(assignment.FieldName())
			if assignment.QUOTED_STRING() != nil {
				name = decodeSPLQuotedOperand(trimMatchingQuotes(assignment.QUOTED_STRING().GetText()))
			}
			if unresolvedSPLFieldName(name) || assignment.Expression() == nil {
				b.opaque(stage, input)
				return
			}
		}
		current := input
		for _, assignment := range ctx.EvalCommand().AllEvalAssignment() {
			b.budget.step()
			name := splFieldName(assignment.FieldName())
			if assignment.QUOTED_STRING() != nil {
				name = decodeSPLQuotedOperand(trimMatchingQuotes(assignment.QUOTED_STRING().GetText()))
			}
			expression := b.extractor.sourceOf(assignment.Expression())
			if expression == "" {
				b.sourceFailure = true
				return
			}
			sourceHint := firstSPLFieldHint(assignment.Expression().GetText(), b.budget)
			id := b.binding(stage, name, "assignment", expression, current)
			b.assignmentSources[id] = sourceHint
			current = b.scope("inherit", current, []string{id}, nil)
		}
		stage.OutputScopeID = current
	case ctx.TableCommand() != nil:
		stage.Effect = "project"
		fields := b.fieldSelectors(ctx.TableCommand().FieldList())
		if len(fields) == 0 {
			b.opaque(stage, input)
			return
		}
		for _, field := range fields {
			if unresolvedSPLFieldName(field.name) {
				b.opaque(stage, input)
				return
			}
		}
		for _, field := range fields {
			if field.source == "" {
				b.sourceFailure = true
				return
			}
			b.binding(stage, field.name, "passthrough", field.source, input)
		}
		stage.OutputScopeID = b.scope("closed", input, stage.BindingIDs, nil)
	case ctx.FieldsCommand() != nil:
		stage.Effect = "project"
		fields := b.fieldList(ctx.FieldsCommand().FieldList())
		valid := len(fields) > 0
		for _, field := range fields {
			if unresolvedSPLFieldName(field) {
				valid = false
				break
			}
		}
		if ctx.FieldsCommand().MINUS() != nil && valid {
			stage.OutputScopeID = b.scope("inherit", input, nil, fields)
		} else {
			b.opaque(stage, input)
		}
	case ctx.RenameCommand() != nil:
		stage.Effect = "rename"
		var oldNames, newNames []string
		for _, spec := range ctx.RenameCommand().AllRenameSpec() {
			fields := spec.AllFieldName()
			if len(fields) == 0 {
				b.opaque(stage, input)
				return
			}
			oldName := splFieldName(fields[0])
			newName := ""
			if len(fields) > 1 {
				newName = splFieldName(fields[1])
			}
			if spec.QUOTED_STRING() != nil {
				newName = decodeSPLQuotedOperand(trimMatchingQuotes(spec.QUOTED_STRING().GetText()))
			}
			if unresolvedSPLFieldName(oldName) || unresolvedSPLFieldName(newName) {
				b.opaque(stage, input)
				return
			}
			oldNames, newNames = append(oldNames, oldName), append(newNames, newName)
		}
		for i, oldName := range oldNames {
			for j, newName := range newNames {
				if i != j && (oldName == newName || oldName == oldNames[j] || newNames[i] == newName) {
					b.opaque(stage, input)
					return
				}
			}
		}
		for i, oldName := range oldNames {
			b.binding(stage, newNames[i], "alias", oldName, input)
		}
		stage.OutputScopeID = b.scope("inherit", input, stage.BindingIDs, oldNames)
	case ctx.StatsCommand() != nil, ctx.EventstatsCommand() != nil, ctx.StreamstatsCommand() != nil:
		stage.Effect = "aggregate"
		var functions []IStatsFunctionContext
		var groups IFieldListContext
		groupBindingCount := 0
		switch {
		case ctx.StatsCommand() != nil:
			functions, groups = ctx.StatsCommand().AllStatsFunction(), ctx.StatsCommand().FieldList()
		case ctx.EventstatsCommand() != nil:
			functions, groups = ctx.EventstatsCommand().AllStatsFunction(), ctx.EventstatsCommand().FieldList()
		case ctx.StreamstatsCommand() != nil:
			functions, groups = ctx.StreamstatsCommand().AllStatsFunction(), ctx.StreamstatsCommand().FieldList()
		}
		if stage.Command == "stats" || stage.Command == "eventstats" || stage.Command == "streamstats" {
			// A bare non-count function means implicit wildcard aggregation.
			// Its output fields depend on the input schema, so inspect the
			// entire stage before publishing any finite group/metric bindings.
			for _, fn := range functions {
				b.budget.step()
				if fn.LPAREN() == nil && fn.Expression() == nil && fn.IDENTIFIER() != nil {
					token := fn.IDENTIFIER().GetSymbol()
					// Identifier tokens are ASCII; reserve the comparison's
					// source materialization/scan before reading its text.
					for i := token.GetStart(); i <= token.GetStop(); i++ {
						b.budget.step()
					}
					if !strings.EqualFold(token.GetText(), "count") {
						b.opaque(stage, input)
						return
					}
				}
			}
		}
		for _, aggregation := range b.extractor.aggregations {
			b.budget.step()
			if aggregation.PipeStage == stage.PipeStage && aggregation.Command == stage.Command &&
				unresolvedSPLFieldName(aggregation.Column) {
				b.opaque(stage, input)
				return
			}
		}
		if stage.Command == "stats" {
			groupFields := b.fieldSelectors(groups)
			for _, field := range groupFields {
				if unresolvedSPLFieldName(field.name) {
					b.opaque(stage, input)
					return
				}
			}
			for _, field := range groupFields {
				if field.source == "" {
					b.sourceFailure = true
					return
				}
				b.binding(stage, field.name, "group", field.source, input)
			}
			groupBindingCount = len(stage.BindingIDs)
		}
		for _, fn := range functions {
			b.budget.step()
			for i := range b.extractor.aggregations {
				a := &b.extractor.aggregations[i]
				if a.PipeStage != stage.PipeStage || a.Command != stage.Command || a.BindingID != "" {
					continue
				}
				expression := b.extractor.sourceOf(fn)
				if expression == "" {
					b.sourceFailure = true
					return
				}
				a.StageID = stage.ID
				a.BindingID = b.binding(stage, a.Column, "aggregate", expression, input)
				b.budget.step()
				b.argumentlessAggregates[a.BindingID] = fn.Expression() == nil && fn.AS() == nil
				break
			}
		}
		if stage.Command == "stats" {
			// Keep binding ID order (and its existing duplicate-name lookup
			// behavior), but publish ordinals in the authored aggregate-then-BY order.
			aggregateCount := len(stage.BindingIDs) - groupBindingCount
			for i, id := range stage.BindingIDs {
				b.budget.step()
				ordinal := i - groupBindingCount
				if i < groupBindingCount {
					ordinal = aggregateCount + i
				}
				b.flow.Bindings[b.bindings[id]].Ordinal = ordinal
			}
		}
		if stage.Command == "stats" {
			stage.OutputScopeID = b.scope("closed", input, stage.BindingIDs, nil)
		} else {
			stage.OutputScopeID = b.scope("inherit", input, stage.BindingIDs, nil)
		}
	case ctx.SortCommand() != nil, ctx.HeadCommand() != nil, ctx.TailCommand() != nil, ctx.DedupCommand() != nil:
		stage.Effect = "retain"
	case ctx.JoinCommand() != nil, ctx.AppendCommand() != nil:
		b.opaque(stage, input)
		joinIndex := 0
		for _, earlier := range b.flow.Stages {
			if earlier.Command == "join" || earlier.Command == "append" {
				joinIndex++
			}
		}
		var childCtx *SubsearchContext
		if ctx.JoinCommand() != nil && ctx.JoinCommand().Subsearch() != nil {
			childCtx = ctx.JoinCommand().Subsearch().(*SubsearchContext)
		} else if ctx.AppendCommand() != nil && ctx.AppendCommand().Subsearch() != nil {
			childCtx = ctx.AppendCommand().Subsearch().(*SubsearchContext)
		}
		if joinIndex < len(b.extractor.joins) && childCtx != nil {
			child := b.extractor.joins[joinIndex].Subsearch
			if child == nil || child.Dataflow == nil {
				b.sourceFailure = true
				return
			}
			path := b.extractor.canonicalChildren[childCtx]
			if path != fmt.Sprintf("joins/%d", joinIndex) {
				b.sourceFailure = true
				return
			}
			b.budget.reserveOutput(1)
			stage.Inputs = append(stage.Inputs, ScopeReference{QueryPath: path, ScopeID: child.Dataflow.OutputScopeID})
		}
	default:
		b.opaque(stage, input)
	}
}

func (b *splDataflowBuilder) opaque(stage *DataflowStage, input string) {
	stage.Effect = "opaque"
	stage.BindingIDs = nil
	stage.OutputScopeID = b.scope("opaque", input, nil, nil)
	b.diagnostic("unsupported_stage_dataflow", stage.ID, "unsupported field scope for "+stage.Command)
}

func cloneFieldBindingRef(ref *FieldBindingRef, budget *parseBudget) *FieldBindingRef {
	if ref == nil {
		return nil
	}
	budget.reserveOutput(1)
	copy := *ref
	return &copy
}

func effectiveFieldBindingIdentity(ref *FieldBindingRef, pending pendingFieldBinding) string {
	if ref != nil {
		return fieldBindingIdentity(ref)
	}
	if !pending.valid {
		return "0"
	}
	return fieldBindingIdentity(&pending.value)
}

func materializeFieldBindingRef(ref **FieldBindingRef, pending *pendingFieldBinding, budget *parseBudget) {
	if !pending.valid {
		return
	}
	budget.reserveOutput(1)
	value := pending.value
	*ref = &value
	*pending = pendingFieldBinding{}
}

func materializeOperandBindings(operand *Operand, budget *parseBudget) {
	if operand == nil {
		return
	}
	budget.step()
	materializeFieldBindingRef(&operand.FieldBinding, &operand.pendingFieldBinding, budget)
	for i := range operand.Items {
		materializeOperandBindings(&operand.Items[i], budget)
	}
}

func materializeConditionBindings(conditions []Condition, budget *parseBudget) {
	for i := range conditions {
		budget.step()
		materializeFieldBindingRef(&conditions[i].FieldBinding, &conditions[i].pendingFieldBinding, budget)
		materializeOperandBindings(conditions[i].LHS, budget)
		materializeOperandBindings(conditions[i].RHS, budget)
	}
}

func cloneQueryDataflow(flow *QueryDataflow, budget *parseBudget) *QueryDataflow {
	if flow == nil {
		return nil
	}
	budget.reserveOutput(1 + len(flow.Scopes) + len(flow.Stages) + len(flow.Bindings) + len(flow.Diagnostics))
	copy := *flow
	copy.Scopes = append([]FieldScope(nil), flow.Scopes...)
	for i := range copy.Scopes {
		budget.step()
		copy.Scopes[i].BindingIDs = cloneSemanticStrings(flow.Scopes[i].BindingIDs, budget)
		copy.Scopes[i].RemovedFields = cloneSemanticStrings(flow.Scopes[i].RemovedFields, budget)
	}
	copy.Stages = append([]DataflowStage(nil), flow.Stages...)
	for i := range copy.Stages {
		budget.step()
		budget.reserveOutput(len(flow.Stages[i].Inputs))
		copy.Stages[i].Inputs = append([]ScopeReference(nil), flow.Stages[i].Inputs...)
		copy.Stages[i].BindingIDs = cloneSemanticStrings(flow.Stages[i].BindingIDs, budget)
	}
	copy.Bindings = append([]FieldBinding(nil), flow.Bindings...)
	copy.Diagnostics = append([]DataflowDiagnostic(nil), flow.Diagnostics...)
	return &copy
}

func finalizeDataflow(result *ParseResult, budget *parseBudget) error {
	if result == nil || result.Dataflow == nil {
		return nil
	}
	flow := result.Dataflow
	scopes := make(map[string]FieldScope, len(flow.Scopes))
	stages := make(map[string]DataflowStage, len(flow.Stages))
	bindings := make(map[string]FieldBinding, len(flow.Bindings))
	for _, scope := range flow.Scopes {
		budget.step()
		if scope.ID == "" {
			return fmt.Errorf("empty scope ID")
		}
		if _, exists := scopes[scope.ID]; exists {
			return fmt.Errorf("duplicate scope %s", scope.ID)
		}
		scopes[scope.ID] = scope
	}
	for _, stage := range flow.Stages {
		budget.step()
		if stage.ID == "" || stage.Source == "" {
			return fmt.Errorf("invalid stage source or ID")
		}
		if _, exists := stages[stage.ID]; exists {
			return fmt.Errorf("duplicate stage %s", stage.ID)
		}
		stages[stage.ID] = stage
	}
	for _, binding := range flow.Bindings {
		budget.step()
		if binding.ID == "" {
			return fmt.Errorf("empty binding ID")
		}
		if _, exists := bindings[binding.ID]; exists {
			return fmt.Errorf("duplicate binding %s", binding.ID)
		}
		bindings[binding.ID] = binding
		if _, ok := stages[binding.StageID]; !ok {
			return fmt.Errorf("binding %s has missing stage", binding.ID)
		}
		if _, ok := scopes[binding.InputScopeID]; !ok {
			return fmt.Errorf("binding %s has missing input scope", binding.ID)
		}
	}
	if _, ok := scopes[flow.OutputScopeID]; !ok {
		return fmt.Errorf("missing output scope")
	}
	visit := make(map[string]uint8, len(scopes))
	var checkParent func(string) error
	checkParent = func(id string) error {
		budget.step()
		if visit[id] == 1 {
			return fmt.Errorf("scope cycle at %s", id)
		}
		if visit[id] == 2 {
			return nil
		}
		visit[id] = 1
		scope := scopes[id]
		if scope.ParentID != "" {
			if _, ok := scopes[scope.ParentID]; !ok {
				return fmt.Errorf("scope %s has missing parent", id)
			}
			if err := checkParent(scope.ParentID); err != nil {
				return err
			}
		}
		for _, bindingID := range scope.BindingIDs {
			budget.step()
			if _, ok := bindings[bindingID]; !ok {
				return fmt.Errorf("scope %s has missing binding", id)
			}
		}
		visit[id] = 2
		return nil
	}
	for id := range scopes {
		if err := checkParent(id); err != nil {
			return err
		}
	}
	for _, scope := range flow.Scopes {
		for _, bindingID := range scope.BindingIDs {
			budget.step()
			binding := bindings[bindingID]
			if binding.InputScopeID == scope.ID {
				return fmt.Errorf("binding %s reads its own scope", bindingID)
			}
		}
	}
	for _, stage := range flow.Stages {
		budget.step()
		if _, ok := scopes[stage.OutputScopeID]; !ok {
			return fmt.Errorf("stage %s has missing output scope", stage.ID)
		}
		for _, id := range stage.BindingIDs {
			if binding, ok := bindings[id]; !ok || binding.StageID != stage.ID {
				return fmt.Errorf("stage %s has invalid binding", stage.ID)
			}
		}
		for _, input := range stage.Inputs {
			if input.QueryPath == "" {
				if _, ok := scopes[input.ScopeID]; !ok {
					return fmt.Errorf("stage %s has missing local input", stage.ID)
				}
				continue
			}
			child, err := resolveDataflowPath(result, input.QueryPath, budget)
			if err != nil || child == nil || child.Dataflow == nil || child.Dataflow.OutputScopeID != input.ScopeID {
				return fmt.Errorf("stage %s has invalid child input %s", stage.ID, input.QueryPath)
			}
		}
	}
	lineage := make(map[string][]string, len(scopes))
	for _, scope := range flow.Scopes {
		budget.step()
		if scope.ParentID != "" {
			lineage[scope.ID] = append(lineage[scope.ID], scope.ParentID)
		}
		for _, id := range scope.BindingIDs {
			lineage[scope.ID] = append(lineage[scope.ID], bindings[id].InputScopeID)
		}
	}
	for _, stage := range flow.Stages {
		budget.step()
		for _, input := range stage.Inputs {
			if input.QueryPath == "" {
				lineage[stage.OutputScopeID] = append(lineage[stage.OutputScopeID], input.ScopeID)
			}
		}
	}
	lineageState := make(map[string]uint8, len(scopes))
	var checkLineage func(string) error
	checkLineage = func(id string) error {
		budget.step()
		if lineageState[id] == 1 {
			return fmt.Errorf("scope/input lineage cycle at %s", id)
		}
		if lineageState[id] == 2 {
			return nil
		}
		lineageState[id] = 1
		for _, input := range lineage[id] {
			if err := checkLineage(input); err != nil {
				return err
			}
		}
		lineageState[id] = 2
		return nil
	}
	for id := range scopes {
		if err := checkLineage(id); err != nil {
			return err
		}
	}
	checkRef := func(ref *FieldBindingRef) error {
		if ref == nil {
			return nil
		}
		budget.step()
		if _, ok := scopes[ref.ScopeID]; !ok {
			return fmt.Errorf("field reference has missing scope %s", ref.ScopeID)
		}
		if ref.Kind == "binding" {
			if _, ok := bindings[ref.BindingID]; !ok {
				return fmt.Errorf("field reference has missing binding %s", ref.BindingID)
			}
		} else if ref.BindingID != "" {
			return fmt.Errorf("nonbinding reference has binding ID")
		}
		return nil
	}
	var checkOperand func(*Operand) error
	checkOperand = func(operand *Operand) error {
		if operand == nil {
			return nil
		}
		if err := checkRef(operand.FieldBinding); err != nil {
			return err
		}
		for i := range operand.Items {
			if err := checkOperand(&operand.Items[i]); err != nil {
				return err
			}
		}
		return nil
	}
	for _, condition := range result.Conditions {
		budget.step()
		if condition.StageID == "" {
			continue
		} // resource compatibility conditions are not event leaves.
		if _, ok := stages[condition.StageID]; !ok {
			return fmt.Errorf("condition has missing stage")
		}
		if _, ok := scopes[condition.InputScopeID]; !ok {
			return fmt.Errorf("condition has missing scope")
		}
		if err := checkRef(condition.FieldBinding); err != nil {
			return err
		}
		if err := checkOperand(condition.LHS); err != nil {
			return err
		}
		if err := checkOperand(condition.RHS); err != nil {
			return err
		}
	}
	var checkExpression func(*ConditionExpression) error
	checkExpression = func(expression *ConditionExpression) error {
		if expression == nil {
			return nil
		}
		budget.step()
		if scalar := expression.Scalar; scalar != nil {
			if _, ok := stages[scalar.StageID]; !ok {
				return fmt.Errorf("scalar has missing stage %s", scalar.StageID)
			}
			if _, ok := scopes[scalar.InputScopeID]; !ok {
				return fmt.Errorf("scalar has missing input scope %s", scalar.InputScopeID)
			}
		}
		for _, child := range expression.Children {
			if err := checkExpression(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := checkExpression(result.Expression); err != nil {
		return err
	}
	for _, aggregation := range result.Aggregations {
		budget.step()
		if aggregation.StageID == "" {
			continue
		} // legacy unsupported aggregation command.
		if _, ok := stages[aggregation.StageID]; !ok {
			return fmt.Errorf("aggregation has missing stage")
		}
		if _, ok := bindings[aggregation.BindingID]; !ok {
			return fmt.Errorf("aggregation has missing binding")
		}
	}
	return nil
}

func finalizeSPLDataflowResult(result *ParseResult, budget *parseBudget) *ParseResult {
	if err := finalizeDataflow(result, budget); err != nil {
		return &ParseResult{Conditions: []Condition{}, Commands: []string{}, Errors: []string{"dataflow integrity failed: " + err.Error()}}
	}
	return result
}

func resolveDataflowPath(result *ParseResult, path string, budget *parseBudget) (*ParseResult, error) {
	parts := strings.Split(path, "/")
	if len(parts)%2 != 0 {
		return nil, fmt.Errorf("invalid query path %q", path)
	}
	for i := 0; i < len(parts); i += 2 {
		budget.step()
		index, err := strconv.Atoi(parts[i+1])
		if err != nil || index < 0 {
			return nil, fmt.Errorf("invalid query path %q", path)
		}
		switch parts[i] {
		case "joins":
			if result == nil || index >= len(result.Joins) {
				return nil, fmt.Errorf("missing join %d", index)
			}
			result = result.Joins[index].Subsearch
		case "subsearches":
			if result == nil || index >= len(result.Subsearches) {
				return nil, fmt.Errorf("missing subsearch %d", index)
			}
			result = result.Subsearches[index]
		default:
			return nil, fmt.Errorf("invalid query path %q", path)
		}
		if result == nil {
			return nil, fmt.Errorf("nil child at %q", path)
		}
	}
	return result, nil
}
