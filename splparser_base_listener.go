// Code generated from SPLParser.g4 by ANTLR 4.13.2. DO NOT EDIT.

package spl // SPLParser

import "github.com/antlr4-go/antlr/v4"

// BaseSPLParserListener is a complete listener for a parse tree produced by SPLParser.
type BaseSPLParserListener struct{}

var _ SPLParserListener = &BaseSPLParserListener{}

// VisitTerminal is called when a terminal node is visited.
func (s *BaseSPLParserListener) VisitTerminal(node antlr.TerminalNode) {}

// VisitErrorNode is called when an error node is visited.
func (s *BaseSPLParserListener) VisitErrorNode(node antlr.ErrorNode) {}

// EnterEveryRule is called when any rule is entered.
func (s *BaseSPLParserListener) EnterEveryRule(ctx antlr.ParserRuleContext) {}

// ExitEveryRule is called when any rule is exited.
func (s *BaseSPLParserListener) ExitEveryRule(ctx antlr.ParserRuleContext) {}

// EnterQuery is called when production query is entered.
func (s *BaseSPLParserListener) EnterQuery(ctx *QueryContext) {}

// ExitQuery is called when production query is exited.
func (s *BaseSPLParserListener) ExitQuery(ctx *QueryContext) {}

// EnterPipelineStage is called when production pipelineStage is entered.
func (s *BaseSPLParserListener) EnterPipelineStage(ctx *PipelineStageContext) {}

// ExitPipelineStage is called when production pipelineStage is exited.
func (s *BaseSPLParserListener) ExitPipelineStage(ctx *PipelineStageContext) {}

// EnterSearchCommand is called when production searchCommand is entered.
func (s *BaseSPLParserListener) EnterSearchCommand(ctx *SearchCommandContext) {}

// ExitSearchCommand is called when production searchCommand is exited.
func (s *BaseSPLParserListener) ExitSearchCommand(ctx *SearchCommandContext) {}

// EnterWhereCommand is called when production whereCommand is entered.
func (s *BaseSPLParserListener) EnterWhereCommand(ctx *WhereCommandContext) {}

// ExitWhereCommand is called when production whereCommand is exited.
func (s *BaseSPLParserListener) ExitWhereCommand(ctx *WhereCommandContext) {}

// EnterEvalCommand is called when production evalCommand is entered.
func (s *BaseSPLParserListener) EnterEvalCommand(ctx *EvalCommandContext) {}

// ExitEvalCommand is called when production evalCommand is exited.
func (s *BaseSPLParserListener) ExitEvalCommand(ctx *EvalCommandContext) {}

// EnterEvalAssignment is called when production evalAssignment is entered.
func (s *BaseSPLParserListener) EnterEvalAssignment(ctx *EvalAssignmentContext) {}

// ExitEvalAssignment is called when production evalAssignment is exited.
func (s *BaseSPLParserListener) ExitEvalAssignment(ctx *EvalAssignmentContext) {}

// EnterStatsCommand is called when production statsCommand is entered.
func (s *BaseSPLParserListener) EnterStatsCommand(ctx *StatsCommandContext) {}

// ExitStatsCommand is called when production statsCommand is exited.
func (s *BaseSPLParserListener) ExitStatsCommand(ctx *StatsCommandContext) {}

// EnterStatsFunction is called when production statsFunction is entered.
func (s *BaseSPLParserListener) EnterStatsFunction(ctx *StatsFunctionContext) {}

// ExitStatsFunction is called when production statsFunction is exited.
func (s *BaseSPLParserListener) ExitStatsFunction(ctx *StatsFunctionContext) {}

// EnterTableCommand is called when production tableCommand is entered.
func (s *BaseSPLParserListener) EnterTableCommand(ctx *TableCommandContext) {}

// ExitTableCommand is called when production tableCommand is exited.
func (s *BaseSPLParserListener) ExitTableCommand(ctx *TableCommandContext) {}

// EnterFieldsCommand is called when production fieldsCommand is entered.
func (s *BaseSPLParserListener) EnterFieldsCommand(ctx *FieldsCommandContext) {}

// ExitFieldsCommand is called when production fieldsCommand is exited.
func (s *BaseSPLParserListener) ExitFieldsCommand(ctx *FieldsCommandContext) {}

// EnterRenameCommand is called when production renameCommand is entered.
func (s *BaseSPLParserListener) EnterRenameCommand(ctx *RenameCommandContext) {}

// ExitRenameCommand is called when production renameCommand is exited.
func (s *BaseSPLParserListener) ExitRenameCommand(ctx *RenameCommandContext) {}

// EnterRenameSpec is called when production renameSpec is entered.
func (s *BaseSPLParserListener) EnterRenameSpec(ctx *RenameSpecContext) {}

// ExitRenameSpec is called when production renameSpec is exited.
func (s *BaseSPLParserListener) ExitRenameSpec(ctx *RenameSpecContext) {}

// EnterRexCommand is called when production rexCommand is entered.
func (s *BaseSPLParserListener) EnterRexCommand(ctx *RexCommandContext) {}

// ExitRexCommand is called when production rexCommand is exited.
func (s *BaseSPLParserListener) ExitRexCommand(ctx *RexCommandContext) {}

// EnterRexOption is called when production rexOption is entered.
func (s *BaseSPLParserListener) EnterRexOption(ctx *RexOptionContext) {}

// ExitRexOption is called when production rexOption is exited.
func (s *BaseSPLParserListener) ExitRexOption(ctx *RexOptionContext) {}

// EnterDedupCommand is called when production dedupCommand is entered.
func (s *BaseSPLParserListener) EnterDedupCommand(ctx *DedupCommandContext) {}

// ExitDedupCommand is called when production dedupCommand is exited.
func (s *BaseSPLParserListener) ExitDedupCommand(ctx *DedupCommandContext) {}

// EnterDedupOption is called when production dedupOption is entered.
func (s *BaseSPLParserListener) EnterDedupOption(ctx *DedupOptionContext) {}

// ExitDedupOption is called when production dedupOption is exited.
func (s *BaseSPLParserListener) ExitDedupOption(ctx *DedupOptionContext) {}

// EnterSortCommand is called when production sortCommand is entered.
func (s *BaseSPLParserListener) EnterSortCommand(ctx *SortCommandContext) {}

// ExitSortCommand is called when production sortCommand is exited.
func (s *BaseSPLParserListener) ExitSortCommand(ctx *SortCommandContext) {}

// EnterSortField is called when production sortField is entered.
func (s *BaseSPLParserListener) EnterSortField(ctx *SortFieldContext) {}

// ExitSortField is called when production sortField is exited.
func (s *BaseSPLParserListener) ExitSortField(ctx *SortFieldContext) {}

// EnterHeadCommand is called when production headCommand is entered.
func (s *BaseSPLParserListener) EnterHeadCommand(ctx *HeadCommandContext) {}

// ExitHeadCommand is called when production headCommand is exited.
func (s *BaseSPLParserListener) ExitHeadCommand(ctx *HeadCommandContext) {}

// EnterTailCommand is called when production tailCommand is entered.
func (s *BaseSPLParserListener) EnterTailCommand(ctx *TailCommandContext) {}

// ExitTailCommand is called when production tailCommand is exited.
func (s *BaseSPLParserListener) ExitTailCommand(ctx *TailCommandContext) {}

// EnterTopCommand is called when production topCommand is entered.
func (s *BaseSPLParserListener) EnterTopCommand(ctx *TopCommandContext) {}

// ExitTopCommand is called when production topCommand is exited.
func (s *BaseSPLParserListener) ExitTopCommand(ctx *TopCommandContext) {}

// EnterRareCommand is called when production rareCommand is entered.
func (s *BaseSPLParserListener) EnterRareCommand(ctx *RareCommandContext) {}

// ExitRareCommand is called when production rareCommand is exited.
func (s *BaseSPLParserListener) ExitRareCommand(ctx *RareCommandContext) {}

// EnterLookupCommand is called when production lookupCommand is entered.
func (s *BaseSPLParserListener) EnterLookupCommand(ctx *LookupCommandContext) {}

// ExitLookupCommand is called when production lookupCommand is exited.
func (s *BaseSPLParserListener) ExitLookupCommand(ctx *LookupCommandContext) {}

// EnterLookupOption is called when production lookupOption is entered.
func (s *BaseSPLParserListener) EnterLookupOption(ctx *LookupOptionContext) {}

// ExitLookupOption is called when production lookupOption is exited.
func (s *BaseSPLParserListener) ExitLookupOption(ctx *LookupOptionContext) {}

// EnterJoinCommand is called when production joinCommand is entered.
func (s *BaseSPLParserListener) EnterJoinCommand(ctx *JoinCommandContext) {}

// ExitJoinCommand is called when production joinCommand is exited.
func (s *BaseSPLParserListener) ExitJoinCommand(ctx *JoinCommandContext) {}

// EnterJoinOption is called when production joinOption is entered.
func (s *BaseSPLParserListener) EnterJoinOption(ctx *JoinOptionContext) {}

// ExitJoinOption is called when production joinOption is exited.
func (s *BaseSPLParserListener) ExitJoinOption(ctx *JoinOptionContext) {}

// EnterAppendCommand is called when production appendCommand is entered.
func (s *BaseSPLParserListener) EnterAppendCommand(ctx *AppendCommandContext) {}

// ExitAppendCommand is called when production appendCommand is exited.
func (s *BaseSPLParserListener) ExitAppendCommand(ctx *AppendCommandContext) {}

// EnterTransactionCommand is called when production transactionCommand is entered.
func (s *BaseSPLParserListener) EnterTransactionCommand(ctx *TransactionCommandContext) {}

// ExitTransactionCommand is called when production transactionCommand is exited.
func (s *BaseSPLParserListener) ExitTransactionCommand(ctx *TransactionCommandContext) {}

// EnterTransactionOption is called when production transactionOption is entered.
func (s *BaseSPLParserListener) EnterTransactionOption(ctx *TransactionOptionContext) {}

// ExitTransactionOption is called when production transactionOption is exited.
func (s *BaseSPLParserListener) ExitTransactionOption(ctx *TransactionOptionContext) {}

// EnterSpathCommand is called when production spathCommand is entered.
func (s *BaseSPLParserListener) EnterSpathCommand(ctx *SpathCommandContext) {}

// ExitSpathCommand is called when production spathCommand is exited.
func (s *BaseSPLParserListener) ExitSpathCommand(ctx *SpathCommandContext) {}

// EnterSpathOption is called when production spathOption is entered.
func (s *BaseSPLParserListener) EnterSpathOption(ctx *SpathOptionContext) {}

// ExitSpathOption is called when production spathOption is exited.
func (s *BaseSPLParserListener) ExitSpathOption(ctx *SpathOptionContext) {}

// EnterEventstatsCommand is called when production eventstatsCommand is entered.
func (s *BaseSPLParserListener) EnterEventstatsCommand(ctx *EventstatsCommandContext) {}

// ExitEventstatsCommand is called when production eventstatsCommand is exited.
func (s *BaseSPLParserListener) ExitEventstatsCommand(ctx *EventstatsCommandContext) {}

// EnterStreamstatsCommand is called when production streamstatsCommand is entered.
func (s *BaseSPLParserListener) EnterStreamstatsCommand(ctx *StreamstatsCommandContext) {}

// ExitStreamstatsCommand is called when production streamstatsCommand is exited.
func (s *BaseSPLParserListener) ExitStreamstatsCommand(ctx *StreamstatsCommandContext) {}

// EnterTimechartCommand is called when production timechartCommand is entered.
func (s *BaseSPLParserListener) EnterTimechartCommand(ctx *TimechartCommandContext) {}

// ExitTimechartCommand is called when production timechartCommand is exited.
func (s *BaseSPLParserListener) ExitTimechartCommand(ctx *TimechartCommandContext) {}

// EnterTimechartOption is called when production timechartOption is entered.
func (s *BaseSPLParserListener) EnterTimechartOption(ctx *TimechartOptionContext) {}

// ExitTimechartOption is called when production timechartOption is exited.
func (s *BaseSPLParserListener) ExitTimechartOption(ctx *TimechartOptionContext) {}

// EnterChartCommand is called when production chartCommand is entered.
func (s *BaseSPLParserListener) EnterChartCommand(ctx *ChartCommandContext) {}

// ExitChartCommand is called when production chartCommand is exited.
func (s *BaseSPLParserListener) ExitChartCommand(ctx *ChartCommandContext) {}

// EnterFillnullCommand is called when production fillnullCommand is entered.
func (s *BaseSPLParserListener) EnterFillnullCommand(ctx *FillnullCommandContext) {}

// ExitFillnullCommand is called when production fillnullCommand is exited.
func (s *BaseSPLParserListener) ExitFillnullCommand(ctx *FillnullCommandContext) {}

// EnterFillnullOption is called when production fillnullOption is entered.
func (s *BaseSPLParserListener) EnterFillnullOption(ctx *FillnullOptionContext) {}

// ExitFillnullOption is called when production fillnullOption is exited.
func (s *BaseSPLParserListener) ExitFillnullOption(ctx *FillnullOptionContext) {}

// EnterMakemvCommand is called when production makemvCommand is entered.
func (s *BaseSPLParserListener) EnterMakemvCommand(ctx *MakemvCommandContext) {}

// ExitMakemvCommand is called when production makemvCommand is exited.
func (s *BaseSPLParserListener) ExitMakemvCommand(ctx *MakemvCommandContext) {}

// EnterMakemvOption is called when production makemvOption is entered.
func (s *BaseSPLParserListener) EnterMakemvOption(ctx *MakemvOptionContext) {}

// ExitMakemvOption is called when production makemvOption is exited.
func (s *BaseSPLParserListener) ExitMakemvOption(ctx *MakemvOptionContext) {}

// EnterMvexpandCommand is called when production mvexpandCommand is entered.
func (s *BaseSPLParserListener) EnterMvexpandCommand(ctx *MvexpandCommandContext) {}

// ExitMvexpandCommand is called when production mvexpandCommand is exited.
func (s *BaseSPLParserListener) ExitMvexpandCommand(ctx *MvexpandCommandContext) {}

// EnterFormatCommand is called when production formatCommand is entered.
func (s *BaseSPLParserListener) EnterFormatCommand(ctx *FormatCommandContext) {}

// ExitFormatCommand is called when production formatCommand is exited.
func (s *BaseSPLParserListener) ExitFormatCommand(ctx *FormatCommandContext) {}

// EnterFormatOption is called when production formatOption is entered.
func (s *BaseSPLParserListener) EnterFormatOption(ctx *FormatOptionContext) {}

// ExitFormatOption is called when production formatOption is exited.
func (s *BaseSPLParserListener) ExitFormatOption(ctx *FormatOptionContext) {}

// EnterConvertCommand is called when production convertCommand is entered.
func (s *BaseSPLParserListener) EnterConvertCommand(ctx *ConvertCommandContext) {}

// ExitConvertCommand is called when production convertCommand is exited.
func (s *BaseSPLParserListener) ExitConvertCommand(ctx *ConvertCommandContext) {}

// EnterConvertOption is called when production convertOption is entered.
func (s *BaseSPLParserListener) EnterConvertOption(ctx *ConvertOptionContext) {}

// ExitConvertOption is called when production convertOption is exited.
func (s *BaseSPLParserListener) ExitConvertOption(ctx *ConvertOptionContext) {}

// EnterConvertFunction is called when production convertFunction is entered.
func (s *BaseSPLParserListener) EnterConvertFunction(ctx *ConvertFunctionContext) {}

// ExitConvertFunction is called when production convertFunction is exited.
func (s *BaseSPLParserListener) ExitConvertFunction(ctx *ConvertFunctionContext) {}

// EnterBucketCommand is called when production bucketCommand is entered.
func (s *BaseSPLParserListener) EnterBucketCommand(ctx *BucketCommandContext) {}

// ExitBucketCommand is called when production bucketCommand is exited.
func (s *BaseSPLParserListener) ExitBucketCommand(ctx *BucketCommandContext) {}

// EnterBucketOption is called when production bucketOption is entered.
func (s *BaseSPLParserListener) EnterBucketOption(ctx *BucketOptionContext) {}

// ExitBucketOption is called when production bucketOption is exited.
func (s *BaseSPLParserListener) ExitBucketOption(ctx *BucketOptionContext) {}

// EnterGenericCommand is called when production genericCommand is entered.
func (s *BaseSPLParserListener) EnterGenericCommand(ctx *GenericCommandContext) {}

// ExitGenericCommand is called when production genericCommand is exited.
func (s *BaseSPLParserListener) ExitGenericCommand(ctx *GenericCommandContext) {}

// EnterGenericArg is called when production genericArg is entered.
func (s *BaseSPLParserListener) EnterGenericArg(ctx *GenericArgContext) {}

// ExitGenericArg is called when production genericArg is exited.
func (s *BaseSPLParserListener) ExitGenericArg(ctx *GenericArgContext) {}

// EnterSubsearch is called when production subsearch is entered.
func (s *BaseSPLParserListener) EnterSubsearch(ctx *SubsearchContext) {}

// ExitSubsearch is called when production subsearch is exited.
func (s *BaseSPLParserListener) ExitSubsearch(ctx *SubsearchContext) {}

// EnterSearchExpression is called when production searchExpression is entered.
func (s *BaseSPLParserListener) EnterSearchExpression(ctx *SearchExpressionContext) {}

// ExitSearchExpression is called when production searchExpression is exited.
func (s *BaseSPLParserListener) ExitSearchExpression(ctx *SearchExpressionContext) {}

// EnterSearchTerm is called when production searchTerm is entered.
func (s *BaseSPLParserListener) EnterSearchTerm(ctx *SearchTermContext) {}

// ExitSearchTerm is called when production searchTerm is exited.
func (s *BaseSPLParserListener) ExitSearchTerm(ctx *SearchTermContext) {}

// EnterCondition is called when production condition is entered.
func (s *BaseSPLParserListener) EnterCondition(ctx *ConditionContext) {}

// ExitCondition is called when production condition is exited.
func (s *BaseSPLParserListener) ExitCondition(ctx *ConditionContext) {}

// EnterComparisonOp is called when production comparisonOp is entered.
func (s *BaseSPLParserListener) EnterComparisonOp(ctx *ComparisonOpContext) {}

// ExitComparisonOp is called when production comparisonOp is exited.
func (s *BaseSPLParserListener) ExitComparisonOp(ctx *ComparisonOpContext) {}

// EnterLogicalOp is called when production logicalOp is entered.
func (s *BaseSPLParserListener) EnterLogicalOp(ctx *LogicalOpContext) {}

// ExitLogicalOp is called when production logicalOp is exited.
func (s *BaseSPLParserListener) ExitLogicalOp(ctx *LogicalOpContext) {}

// EnterExpression is called when production expression is entered.
func (s *BaseSPLParserListener) EnterExpression(ctx *ExpressionContext) {}

// ExitExpression is called when production expression is exited.
func (s *BaseSPLParserListener) ExitExpression(ctx *ExpressionContext) {}

// EnterOrExpression is called when production orExpression is entered.
func (s *BaseSPLParserListener) EnterOrExpression(ctx *OrExpressionContext) {}

// ExitOrExpression is called when production orExpression is exited.
func (s *BaseSPLParserListener) ExitOrExpression(ctx *OrExpressionContext) {}

// EnterAndExpression is called when production andExpression is entered.
func (s *BaseSPLParserListener) EnterAndExpression(ctx *AndExpressionContext) {}

// ExitAndExpression is called when production andExpression is exited.
func (s *BaseSPLParserListener) ExitAndExpression(ctx *AndExpressionContext) {}

// EnterNotExpression is called when production notExpression is entered.
func (s *BaseSPLParserListener) EnterNotExpression(ctx *NotExpressionContext) {}

// ExitNotExpression is called when production notExpression is exited.
func (s *BaseSPLParserListener) ExitNotExpression(ctx *NotExpressionContext) {}

// EnterComparisonExpression is called when production comparisonExpression is entered.
func (s *BaseSPLParserListener) EnterComparisonExpression(ctx *ComparisonExpressionContext) {}

// ExitComparisonExpression is called when production comparisonExpression is exited.
func (s *BaseSPLParserListener) ExitComparisonExpression(ctx *ComparisonExpressionContext) {}

// EnterAdditiveExpression is called when production additiveExpression is entered.
func (s *BaseSPLParserListener) EnterAdditiveExpression(ctx *AdditiveExpressionContext) {}

// ExitAdditiveExpression is called when production additiveExpression is exited.
func (s *BaseSPLParserListener) ExitAdditiveExpression(ctx *AdditiveExpressionContext) {}

// EnterMultiplicativeExpression is called when production multiplicativeExpression is entered.
func (s *BaseSPLParserListener) EnterMultiplicativeExpression(ctx *MultiplicativeExpressionContext) {}

// ExitMultiplicativeExpression is called when production multiplicativeExpression is exited.
func (s *BaseSPLParserListener) ExitMultiplicativeExpression(ctx *MultiplicativeExpressionContext) {}

// EnterUnaryExpression is called when production unaryExpression is entered.
func (s *BaseSPLParserListener) EnterUnaryExpression(ctx *UnaryExpressionContext) {}

// ExitUnaryExpression is called when production unaryExpression is exited.
func (s *BaseSPLParserListener) ExitUnaryExpression(ctx *UnaryExpressionContext) {}

// EnterPrimaryExpression is called when production primaryExpression is entered.
func (s *BaseSPLParserListener) EnterPrimaryExpression(ctx *PrimaryExpressionContext) {}

// ExitPrimaryExpression is called when production primaryExpression is exited.
func (s *BaseSPLParserListener) ExitPrimaryExpression(ctx *PrimaryExpressionContext) {}

// EnterFunctionCall is called when production functionCall is entered.
func (s *BaseSPLParserListener) EnterFunctionCall(ctx *FunctionCallContext) {}

// ExitFunctionCall is called when production functionCall is exited.
func (s *BaseSPLParserListener) ExitFunctionCall(ctx *FunctionCallContext) {}

// EnterArgumentList is called when production argumentList is entered.
func (s *BaseSPLParserListener) EnterArgumentList(ctx *ArgumentListContext) {}

// ExitArgumentList is called when production argumentList is exited.
func (s *BaseSPLParserListener) ExitArgumentList(ctx *ArgumentListContext) {}

// EnterValue is called when production value is entered.
func (s *BaseSPLParserListener) EnterValue(ctx *ValueContext) {}

// ExitValue is called when production value is exited.
func (s *BaseSPLParserListener) ExitValue(ctx *ValueContext) {}

// EnterColonValue is called when production colonValue is entered.
func (s *BaseSPLParserListener) EnterColonValue(ctx *ColonValueContext) {}

// ExitColonValue is called when production colonValue is exited.
func (s *BaseSPLParserListener) ExitColonValue(ctx *ColonValueContext) {}

// EnterWildcardValue is called when production wildcardValue is entered.
func (s *BaseSPLParserListener) EnterWildcardValue(ctx *WildcardValueContext) {}

// ExitWildcardValue is called when production wildcardValue is exited.
func (s *BaseSPLParserListener) ExitWildcardValue(ctx *WildcardValueContext) {}

// EnterBareWord is called when production bareWord is entered.
func (s *BaseSPLParserListener) EnterBareWord(ctx *BareWordContext) {}

// ExitBareWord is called when production bareWord is exited.
func (s *BaseSPLParserListener) ExitBareWord(ctx *BareWordContext) {}

// EnterFieldName is called when production fieldName is entered.
func (s *BaseSPLParserListener) EnterFieldName(ctx *FieldNameContext) {}

// ExitFieldName is called when production fieldName is exited.
func (s *BaseSPLParserListener) ExitFieldName(ctx *FieldNameContext) {}

// EnterFieldList is called when production fieldList is entered.
func (s *BaseSPLParserListener) EnterFieldList(ctx *FieldListContext) {}

// ExitFieldList is called when production fieldList is exited.
func (s *BaseSPLParserListener) ExitFieldList(ctx *FieldListContext) {}

// EnterFieldOrQuoted is called when production fieldOrQuoted is entered.
func (s *BaseSPLParserListener) EnterFieldOrQuoted(ctx *FieldOrQuotedContext) {}

// ExitFieldOrQuoted is called when production fieldOrQuoted is exited.
func (s *BaseSPLParserListener) ExitFieldOrQuoted(ctx *FieldOrQuotedContext) {}

// EnterValueList is called when production valueList is entered.
func (s *BaseSPLParserListener) EnterValueList(ctx *ValueListContext) {}

// ExitValueList is called when production valueList is exited.
func (s *BaseSPLParserListener) ExitValueList(ctx *ValueListContext) {}
