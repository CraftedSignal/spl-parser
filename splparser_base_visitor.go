// Code generated from SPLParser.g4 by ANTLR 4.13.2. DO NOT EDIT.

package spl

import "github.com/antlr4-go/antlr/v4"

type BaseSPLParserVisitor struct {
	*antlr.BaseParseTreeVisitor
}

func (v *BaseSPLParserVisitor) VisitQuery(ctx *QueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitPipelineStage(ctx *PipelineStageContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitSearchCommand(ctx *SearchCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitWhereCommand(ctx *WhereCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitEvalCommand(ctx *EvalCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitEvalAssignment(ctx *EvalAssignmentContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitStatsCommand(ctx *StatsCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitStatsFunction(ctx *StatsFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitTableCommand(ctx *TableCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitFieldsCommand(ctx *FieldsCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitRenameCommand(ctx *RenameCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitRenameSpec(ctx *RenameSpecContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitRexCommand(ctx *RexCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitRexOption(ctx *RexOptionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitDedupCommand(ctx *DedupCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitDedupOption(ctx *DedupOptionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitSortCommand(ctx *SortCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitSortField(ctx *SortFieldContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitHeadCommand(ctx *HeadCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitTailCommand(ctx *TailCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitTopCommand(ctx *TopCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitRareCommand(ctx *RareCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitLookupCommand(ctx *LookupCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitLookupOption(ctx *LookupOptionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitJoinCommand(ctx *JoinCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitJoinOption(ctx *JoinOptionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitAppendCommand(ctx *AppendCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitTransactionCommand(ctx *TransactionCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitTransactionOption(ctx *TransactionOptionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitSpathCommand(ctx *SpathCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitSpathOption(ctx *SpathOptionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitEventstatsCommand(ctx *EventstatsCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitStreamstatsCommand(ctx *StreamstatsCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitTimechartCommand(ctx *TimechartCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitTimechartOption(ctx *TimechartOptionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitChartCommand(ctx *ChartCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitFillnullCommand(ctx *FillnullCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitFillnullOption(ctx *FillnullOptionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitMakemvCommand(ctx *MakemvCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitMakemvOption(ctx *MakemvOptionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitMvexpandCommand(ctx *MvexpandCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitFormatCommand(ctx *FormatCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitFormatOption(ctx *FormatOptionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitConvertCommand(ctx *ConvertCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitConvertOption(ctx *ConvertOptionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitConvertFunction(ctx *ConvertFunctionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitBucketCommand(ctx *BucketCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitBucketOption(ctx *BucketOptionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitRestCommand(ctx *RestCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitRestArg(ctx *RestArgContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitTstatsCommand(ctx *TstatsCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitTstatsPreOption(ctx *TstatsPreOptionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitTstatsDatamodel(ctx *TstatsDatamodelContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitTstatsPostOption(ctx *TstatsPostOptionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitMstatsCommand(ctx *MstatsCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitInputlookupCommand(ctx *InputlookupCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitInputlookupOption(ctx *InputlookupOptionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitGenericCommand(ctx *GenericCommandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitGenericArg(ctx *GenericArgContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitSubsearch(ctx *SubsearchContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitSearchExpression(ctx *SearchExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitSearchTerm(ctx *SearchTermContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitCondition(ctx *ConditionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitComparisonOp(ctx *ComparisonOpContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitLogicalOp(ctx *LogicalOpContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitExpression(ctx *ExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitOrExpression(ctx *OrExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitAndExpression(ctx *AndExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitNotExpression(ctx *NotExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitComparisonExpression(ctx *ComparisonExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitAdditiveExpression(ctx *AdditiveExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitMultiplicativeExpression(ctx *MultiplicativeExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitUnaryExpression(ctx *UnaryExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitPrimaryExpression(ctx *PrimaryExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitFunctionCall(ctx *FunctionCallContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitArgumentList(ctx *ArgumentListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitValue(ctx *ValueContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitColonValue(ctx *ColonValueContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitExtendedIdentifier(ctx *ExtendedIdentifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitWildcardValue(ctx *WildcardValueContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitBareWord(ctx *BareWordContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitFieldName(ctx *FieldNameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitFieldList(ctx *FieldListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitFieldOrQuoted(ctx *FieldOrQuotedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSPLParserVisitor) VisitValueList(ctx *ValueListContext) interface{} {
	return v.VisitChildren(ctx)
}
