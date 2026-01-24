// Code generated from SPLParser.g4 by ANTLR 4.13.2. DO NOT EDIT.

package spl // SPLParser

import "github.com/antlr4-go/antlr/v4"

// A complete Visitor for a parse tree produced by SPLParser.
type SPLParserVisitor interface {
	antlr.ParseTreeVisitor

	// Visit a parse tree produced by SPLParser#query.
	VisitQuery(ctx *QueryContext) interface{}

	// Visit a parse tree produced by SPLParser#pipelineStage.
	VisitPipelineStage(ctx *PipelineStageContext) interface{}

	// Visit a parse tree produced by SPLParser#searchCommand.
	VisitSearchCommand(ctx *SearchCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#whereCommand.
	VisitWhereCommand(ctx *WhereCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#evalCommand.
	VisitEvalCommand(ctx *EvalCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#evalAssignment.
	VisitEvalAssignment(ctx *EvalAssignmentContext) interface{}

	// Visit a parse tree produced by SPLParser#statsCommand.
	VisitStatsCommand(ctx *StatsCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#statsFunction.
	VisitStatsFunction(ctx *StatsFunctionContext) interface{}

	// Visit a parse tree produced by SPLParser#tableCommand.
	VisitTableCommand(ctx *TableCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#fieldsCommand.
	VisitFieldsCommand(ctx *FieldsCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#renameCommand.
	VisitRenameCommand(ctx *RenameCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#renameSpec.
	VisitRenameSpec(ctx *RenameSpecContext) interface{}

	// Visit a parse tree produced by SPLParser#rexCommand.
	VisitRexCommand(ctx *RexCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#rexOption.
	VisitRexOption(ctx *RexOptionContext) interface{}

	// Visit a parse tree produced by SPLParser#dedupCommand.
	VisitDedupCommand(ctx *DedupCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#dedupOption.
	VisitDedupOption(ctx *DedupOptionContext) interface{}

	// Visit a parse tree produced by SPLParser#sortCommand.
	VisitSortCommand(ctx *SortCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#sortField.
	VisitSortField(ctx *SortFieldContext) interface{}

	// Visit a parse tree produced by SPLParser#headCommand.
	VisitHeadCommand(ctx *HeadCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#tailCommand.
	VisitTailCommand(ctx *TailCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#topCommand.
	VisitTopCommand(ctx *TopCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#rareCommand.
	VisitRareCommand(ctx *RareCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#lookupCommand.
	VisitLookupCommand(ctx *LookupCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#lookupOption.
	VisitLookupOption(ctx *LookupOptionContext) interface{}

	// Visit a parse tree produced by SPLParser#joinCommand.
	VisitJoinCommand(ctx *JoinCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#joinOption.
	VisitJoinOption(ctx *JoinOptionContext) interface{}

	// Visit a parse tree produced by SPLParser#appendCommand.
	VisitAppendCommand(ctx *AppendCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#transactionCommand.
	VisitTransactionCommand(ctx *TransactionCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#transactionOption.
	VisitTransactionOption(ctx *TransactionOptionContext) interface{}

	// Visit a parse tree produced by SPLParser#spathCommand.
	VisitSpathCommand(ctx *SpathCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#spathOption.
	VisitSpathOption(ctx *SpathOptionContext) interface{}

	// Visit a parse tree produced by SPLParser#eventstatsCommand.
	VisitEventstatsCommand(ctx *EventstatsCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#streamstatsCommand.
	VisitStreamstatsCommand(ctx *StreamstatsCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#timechartCommand.
	VisitTimechartCommand(ctx *TimechartCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#timechartOption.
	VisitTimechartOption(ctx *TimechartOptionContext) interface{}

	// Visit a parse tree produced by SPLParser#chartCommand.
	VisitChartCommand(ctx *ChartCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#fillnullCommand.
	VisitFillnullCommand(ctx *FillnullCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#fillnullOption.
	VisitFillnullOption(ctx *FillnullOptionContext) interface{}

	// Visit a parse tree produced by SPLParser#makemvCommand.
	VisitMakemvCommand(ctx *MakemvCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#makemvOption.
	VisitMakemvOption(ctx *MakemvOptionContext) interface{}

	// Visit a parse tree produced by SPLParser#mvexpandCommand.
	VisitMvexpandCommand(ctx *MvexpandCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#formatCommand.
	VisitFormatCommand(ctx *FormatCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#formatOption.
	VisitFormatOption(ctx *FormatOptionContext) interface{}

	// Visit a parse tree produced by SPLParser#convertCommand.
	VisitConvertCommand(ctx *ConvertCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#convertOption.
	VisitConvertOption(ctx *ConvertOptionContext) interface{}

	// Visit a parse tree produced by SPLParser#convertFunction.
	VisitConvertFunction(ctx *ConvertFunctionContext) interface{}

	// Visit a parse tree produced by SPLParser#bucketCommand.
	VisitBucketCommand(ctx *BucketCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#bucketOption.
	VisitBucketOption(ctx *BucketOptionContext) interface{}

	// Visit a parse tree produced by SPLParser#genericCommand.
	VisitGenericCommand(ctx *GenericCommandContext) interface{}

	// Visit a parse tree produced by SPLParser#genericArg.
	VisitGenericArg(ctx *GenericArgContext) interface{}

	// Visit a parse tree produced by SPLParser#subsearch.
	VisitSubsearch(ctx *SubsearchContext) interface{}

	// Visit a parse tree produced by SPLParser#searchExpression.
	VisitSearchExpression(ctx *SearchExpressionContext) interface{}

	// Visit a parse tree produced by SPLParser#searchTerm.
	VisitSearchTerm(ctx *SearchTermContext) interface{}

	// Visit a parse tree produced by SPLParser#condition.
	VisitCondition(ctx *ConditionContext) interface{}

	// Visit a parse tree produced by SPLParser#comparisonOp.
	VisitComparisonOp(ctx *ComparisonOpContext) interface{}

	// Visit a parse tree produced by SPLParser#logicalOp.
	VisitLogicalOp(ctx *LogicalOpContext) interface{}

	// Visit a parse tree produced by SPLParser#expression.
	VisitExpression(ctx *ExpressionContext) interface{}

	// Visit a parse tree produced by SPLParser#orExpression.
	VisitOrExpression(ctx *OrExpressionContext) interface{}

	// Visit a parse tree produced by SPLParser#andExpression.
	VisitAndExpression(ctx *AndExpressionContext) interface{}

	// Visit a parse tree produced by SPLParser#notExpression.
	VisitNotExpression(ctx *NotExpressionContext) interface{}

	// Visit a parse tree produced by SPLParser#comparisonExpression.
	VisitComparisonExpression(ctx *ComparisonExpressionContext) interface{}

	// Visit a parse tree produced by SPLParser#additiveExpression.
	VisitAdditiveExpression(ctx *AdditiveExpressionContext) interface{}

	// Visit a parse tree produced by SPLParser#multiplicativeExpression.
	VisitMultiplicativeExpression(ctx *MultiplicativeExpressionContext) interface{}

	// Visit a parse tree produced by SPLParser#unaryExpression.
	VisitUnaryExpression(ctx *UnaryExpressionContext) interface{}

	// Visit a parse tree produced by SPLParser#primaryExpression.
	VisitPrimaryExpression(ctx *PrimaryExpressionContext) interface{}

	// Visit a parse tree produced by SPLParser#functionCall.
	VisitFunctionCall(ctx *FunctionCallContext) interface{}

	// Visit a parse tree produced by SPLParser#argumentList.
	VisitArgumentList(ctx *ArgumentListContext) interface{}

	// Visit a parse tree produced by SPLParser#value.
	VisitValue(ctx *ValueContext) interface{}

	// Visit a parse tree produced by SPLParser#colonValue.
	VisitColonValue(ctx *ColonValueContext) interface{}

	// Visit a parse tree produced by SPLParser#wildcardValue.
	VisitWildcardValue(ctx *WildcardValueContext) interface{}

	// Visit a parse tree produced by SPLParser#bareWord.
	VisitBareWord(ctx *BareWordContext) interface{}

	// Visit a parse tree produced by SPLParser#fieldName.
	VisitFieldName(ctx *FieldNameContext) interface{}

	// Visit a parse tree produced by SPLParser#fieldList.
	VisitFieldList(ctx *FieldListContext) interface{}

	// Visit a parse tree produced by SPLParser#fieldOrQuoted.
	VisitFieldOrQuoted(ctx *FieldOrQuotedContext) interface{}

	// Visit a parse tree produced by SPLParser#valueList.
	VisitValueList(ctx *ValueListContext) interface{}
}
