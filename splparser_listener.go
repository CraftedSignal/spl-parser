// Code generated from SPLParser.g4 by ANTLR 4.13.2. DO NOT EDIT.

package spl // SPLParser

import "github.com/antlr4-go/antlr/v4"

// SPLParserListener is a complete listener for a parse tree produced by SPLParser.
type SPLParserListener interface {
	antlr.ParseTreeListener

	// EnterQuery is called when entering the query production.
	EnterQuery(c *QueryContext)

	// EnterPipelineStage is called when entering the pipelineStage production.
	EnterPipelineStage(c *PipelineStageContext)

	// EnterSearchCommand is called when entering the searchCommand production.
	EnterSearchCommand(c *SearchCommandContext)

	// EnterWhereCommand is called when entering the whereCommand production.
	EnterWhereCommand(c *WhereCommandContext)

	// EnterEvalCommand is called when entering the evalCommand production.
	EnterEvalCommand(c *EvalCommandContext)

	// EnterEvalAssignment is called when entering the evalAssignment production.
	EnterEvalAssignment(c *EvalAssignmentContext)

	// EnterStatsCommand is called when entering the statsCommand production.
	EnterStatsCommand(c *StatsCommandContext)

	// EnterStatsFunction is called when entering the statsFunction production.
	EnterStatsFunction(c *StatsFunctionContext)

	// EnterTableCommand is called when entering the tableCommand production.
	EnterTableCommand(c *TableCommandContext)

	// EnterFieldsCommand is called when entering the fieldsCommand production.
	EnterFieldsCommand(c *FieldsCommandContext)

	// EnterRenameCommand is called when entering the renameCommand production.
	EnterRenameCommand(c *RenameCommandContext)

	// EnterRenameSpec is called when entering the renameSpec production.
	EnterRenameSpec(c *RenameSpecContext)

	// EnterRexCommand is called when entering the rexCommand production.
	EnterRexCommand(c *RexCommandContext)

	// EnterRexOption is called when entering the rexOption production.
	EnterRexOption(c *RexOptionContext)

	// EnterDedupCommand is called when entering the dedupCommand production.
	EnterDedupCommand(c *DedupCommandContext)

	// EnterDedupOption is called when entering the dedupOption production.
	EnterDedupOption(c *DedupOptionContext)

	// EnterSortCommand is called when entering the sortCommand production.
	EnterSortCommand(c *SortCommandContext)

	// EnterSortField is called when entering the sortField production.
	EnterSortField(c *SortFieldContext)

	// EnterHeadCommand is called when entering the headCommand production.
	EnterHeadCommand(c *HeadCommandContext)

	// EnterTailCommand is called when entering the tailCommand production.
	EnterTailCommand(c *TailCommandContext)

	// EnterTopCommand is called when entering the topCommand production.
	EnterTopCommand(c *TopCommandContext)

	// EnterRareCommand is called when entering the rareCommand production.
	EnterRareCommand(c *RareCommandContext)

	// EnterLookupCommand is called when entering the lookupCommand production.
	EnterLookupCommand(c *LookupCommandContext)

	// EnterLookupOption is called when entering the lookupOption production.
	EnterLookupOption(c *LookupOptionContext)

	// EnterJoinCommand is called when entering the joinCommand production.
	EnterJoinCommand(c *JoinCommandContext)

	// EnterJoinOption is called when entering the joinOption production.
	EnterJoinOption(c *JoinOptionContext)

	// EnterAppendCommand is called when entering the appendCommand production.
	EnterAppendCommand(c *AppendCommandContext)

	// EnterTransactionCommand is called when entering the transactionCommand production.
	EnterTransactionCommand(c *TransactionCommandContext)

	// EnterTransactionOption is called when entering the transactionOption production.
	EnterTransactionOption(c *TransactionOptionContext)

	// EnterSpathCommand is called when entering the spathCommand production.
	EnterSpathCommand(c *SpathCommandContext)

	// EnterSpathOption is called when entering the spathOption production.
	EnterSpathOption(c *SpathOptionContext)

	// EnterEventstatsCommand is called when entering the eventstatsCommand production.
	EnterEventstatsCommand(c *EventstatsCommandContext)

	// EnterStreamstatsCommand is called when entering the streamstatsCommand production.
	EnterStreamstatsCommand(c *StreamstatsCommandContext)

	// EnterTimechartCommand is called when entering the timechartCommand production.
	EnterTimechartCommand(c *TimechartCommandContext)

	// EnterTimechartOption is called when entering the timechartOption production.
	EnterTimechartOption(c *TimechartOptionContext)

	// EnterChartCommand is called when entering the chartCommand production.
	EnterChartCommand(c *ChartCommandContext)

	// EnterFillnullCommand is called when entering the fillnullCommand production.
	EnterFillnullCommand(c *FillnullCommandContext)

	// EnterFillnullOption is called when entering the fillnullOption production.
	EnterFillnullOption(c *FillnullOptionContext)

	// EnterMakemvCommand is called when entering the makemvCommand production.
	EnterMakemvCommand(c *MakemvCommandContext)

	// EnterMakemvOption is called when entering the makemvOption production.
	EnterMakemvOption(c *MakemvOptionContext)

	// EnterMvexpandCommand is called when entering the mvexpandCommand production.
	EnterMvexpandCommand(c *MvexpandCommandContext)

	// EnterFormatCommand is called when entering the formatCommand production.
	EnterFormatCommand(c *FormatCommandContext)

	// EnterFormatOption is called when entering the formatOption production.
	EnterFormatOption(c *FormatOptionContext)

	// EnterConvertCommand is called when entering the convertCommand production.
	EnterConvertCommand(c *ConvertCommandContext)

	// EnterConvertOption is called when entering the convertOption production.
	EnterConvertOption(c *ConvertOptionContext)

	// EnterConvertFunction is called when entering the convertFunction production.
	EnterConvertFunction(c *ConvertFunctionContext)

	// EnterBucketCommand is called when entering the bucketCommand production.
	EnterBucketCommand(c *BucketCommandContext)

	// EnterBucketOption is called when entering the bucketOption production.
	EnterBucketOption(c *BucketOptionContext)

	// EnterGenericCommand is called when entering the genericCommand production.
	EnterGenericCommand(c *GenericCommandContext)

	// EnterGenericArg is called when entering the genericArg production.
	EnterGenericArg(c *GenericArgContext)

	// EnterSubsearch is called when entering the subsearch production.
	EnterSubsearch(c *SubsearchContext)

	// EnterSearchExpression is called when entering the searchExpression production.
	EnterSearchExpression(c *SearchExpressionContext)

	// EnterSearchTerm is called when entering the searchTerm production.
	EnterSearchTerm(c *SearchTermContext)

	// EnterCondition is called when entering the condition production.
	EnterCondition(c *ConditionContext)

	// EnterComparisonOp is called when entering the comparisonOp production.
	EnterComparisonOp(c *ComparisonOpContext)

	// EnterLogicalOp is called when entering the logicalOp production.
	EnterLogicalOp(c *LogicalOpContext)

	// EnterExpression is called when entering the expression production.
	EnterExpression(c *ExpressionContext)

	// EnterOrExpression is called when entering the orExpression production.
	EnterOrExpression(c *OrExpressionContext)

	// EnterAndExpression is called when entering the andExpression production.
	EnterAndExpression(c *AndExpressionContext)

	// EnterNotExpression is called when entering the notExpression production.
	EnterNotExpression(c *NotExpressionContext)

	// EnterComparisonExpression is called when entering the comparisonExpression production.
	EnterComparisonExpression(c *ComparisonExpressionContext)

	// EnterAdditiveExpression is called when entering the additiveExpression production.
	EnterAdditiveExpression(c *AdditiveExpressionContext)

	// EnterMultiplicativeExpression is called when entering the multiplicativeExpression production.
	EnterMultiplicativeExpression(c *MultiplicativeExpressionContext)

	// EnterUnaryExpression is called when entering the unaryExpression production.
	EnterUnaryExpression(c *UnaryExpressionContext)

	// EnterPrimaryExpression is called when entering the primaryExpression production.
	EnterPrimaryExpression(c *PrimaryExpressionContext)

	// EnterFunctionCall is called when entering the functionCall production.
	EnterFunctionCall(c *FunctionCallContext)

	// EnterArgumentList is called when entering the argumentList production.
	EnterArgumentList(c *ArgumentListContext)

	// EnterValue is called when entering the value production.
	EnterValue(c *ValueContext)

	// EnterColonValue is called when entering the colonValue production.
	EnterColonValue(c *ColonValueContext)

	// EnterWildcardValue is called when entering the wildcardValue production.
	EnterWildcardValue(c *WildcardValueContext)

	// EnterBareWord is called when entering the bareWord production.
	EnterBareWord(c *BareWordContext)

	// EnterFieldName is called when entering the fieldName production.
	EnterFieldName(c *FieldNameContext)

	// EnterFieldList is called when entering the fieldList production.
	EnterFieldList(c *FieldListContext)

	// EnterFieldOrQuoted is called when entering the fieldOrQuoted production.
	EnterFieldOrQuoted(c *FieldOrQuotedContext)

	// EnterValueList is called when entering the valueList production.
	EnterValueList(c *ValueListContext)

	// ExitQuery is called when exiting the query production.
	ExitQuery(c *QueryContext)

	// ExitPipelineStage is called when exiting the pipelineStage production.
	ExitPipelineStage(c *PipelineStageContext)

	// ExitSearchCommand is called when exiting the searchCommand production.
	ExitSearchCommand(c *SearchCommandContext)

	// ExitWhereCommand is called when exiting the whereCommand production.
	ExitWhereCommand(c *WhereCommandContext)

	// ExitEvalCommand is called when exiting the evalCommand production.
	ExitEvalCommand(c *EvalCommandContext)

	// ExitEvalAssignment is called when exiting the evalAssignment production.
	ExitEvalAssignment(c *EvalAssignmentContext)

	// ExitStatsCommand is called when exiting the statsCommand production.
	ExitStatsCommand(c *StatsCommandContext)

	// ExitStatsFunction is called when exiting the statsFunction production.
	ExitStatsFunction(c *StatsFunctionContext)

	// ExitTableCommand is called when exiting the tableCommand production.
	ExitTableCommand(c *TableCommandContext)

	// ExitFieldsCommand is called when exiting the fieldsCommand production.
	ExitFieldsCommand(c *FieldsCommandContext)

	// ExitRenameCommand is called when exiting the renameCommand production.
	ExitRenameCommand(c *RenameCommandContext)

	// ExitRenameSpec is called when exiting the renameSpec production.
	ExitRenameSpec(c *RenameSpecContext)

	// ExitRexCommand is called when exiting the rexCommand production.
	ExitRexCommand(c *RexCommandContext)

	// ExitRexOption is called when exiting the rexOption production.
	ExitRexOption(c *RexOptionContext)

	// ExitDedupCommand is called when exiting the dedupCommand production.
	ExitDedupCommand(c *DedupCommandContext)

	// ExitDedupOption is called when exiting the dedupOption production.
	ExitDedupOption(c *DedupOptionContext)

	// ExitSortCommand is called when exiting the sortCommand production.
	ExitSortCommand(c *SortCommandContext)

	// ExitSortField is called when exiting the sortField production.
	ExitSortField(c *SortFieldContext)

	// ExitHeadCommand is called when exiting the headCommand production.
	ExitHeadCommand(c *HeadCommandContext)

	// ExitTailCommand is called when exiting the tailCommand production.
	ExitTailCommand(c *TailCommandContext)

	// ExitTopCommand is called when exiting the topCommand production.
	ExitTopCommand(c *TopCommandContext)

	// ExitRareCommand is called when exiting the rareCommand production.
	ExitRareCommand(c *RareCommandContext)

	// ExitLookupCommand is called when exiting the lookupCommand production.
	ExitLookupCommand(c *LookupCommandContext)

	// ExitLookupOption is called when exiting the lookupOption production.
	ExitLookupOption(c *LookupOptionContext)

	// ExitJoinCommand is called when exiting the joinCommand production.
	ExitJoinCommand(c *JoinCommandContext)

	// ExitJoinOption is called when exiting the joinOption production.
	ExitJoinOption(c *JoinOptionContext)

	// ExitAppendCommand is called when exiting the appendCommand production.
	ExitAppendCommand(c *AppendCommandContext)

	// ExitTransactionCommand is called when exiting the transactionCommand production.
	ExitTransactionCommand(c *TransactionCommandContext)

	// ExitTransactionOption is called when exiting the transactionOption production.
	ExitTransactionOption(c *TransactionOptionContext)

	// ExitSpathCommand is called when exiting the spathCommand production.
	ExitSpathCommand(c *SpathCommandContext)

	// ExitSpathOption is called when exiting the spathOption production.
	ExitSpathOption(c *SpathOptionContext)

	// ExitEventstatsCommand is called when exiting the eventstatsCommand production.
	ExitEventstatsCommand(c *EventstatsCommandContext)

	// ExitStreamstatsCommand is called when exiting the streamstatsCommand production.
	ExitStreamstatsCommand(c *StreamstatsCommandContext)

	// ExitTimechartCommand is called when exiting the timechartCommand production.
	ExitTimechartCommand(c *TimechartCommandContext)

	// ExitTimechartOption is called when exiting the timechartOption production.
	ExitTimechartOption(c *TimechartOptionContext)

	// ExitChartCommand is called when exiting the chartCommand production.
	ExitChartCommand(c *ChartCommandContext)

	// ExitFillnullCommand is called when exiting the fillnullCommand production.
	ExitFillnullCommand(c *FillnullCommandContext)

	// ExitFillnullOption is called when exiting the fillnullOption production.
	ExitFillnullOption(c *FillnullOptionContext)

	// ExitMakemvCommand is called when exiting the makemvCommand production.
	ExitMakemvCommand(c *MakemvCommandContext)

	// ExitMakemvOption is called when exiting the makemvOption production.
	ExitMakemvOption(c *MakemvOptionContext)

	// ExitMvexpandCommand is called when exiting the mvexpandCommand production.
	ExitMvexpandCommand(c *MvexpandCommandContext)

	// ExitFormatCommand is called when exiting the formatCommand production.
	ExitFormatCommand(c *FormatCommandContext)

	// ExitFormatOption is called when exiting the formatOption production.
	ExitFormatOption(c *FormatOptionContext)

	// ExitConvertCommand is called when exiting the convertCommand production.
	ExitConvertCommand(c *ConvertCommandContext)

	// ExitConvertOption is called when exiting the convertOption production.
	ExitConvertOption(c *ConvertOptionContext)

	// ExitConvertFunction is called when exiting the convertFunction production.
	ExitConvertFunction(c *ConvertFunctionContext)

	// ExitBucketCommand is called when exiting the bucketCommand production.
	ExitBucketCommand(c *BucketCommandContext)

	// ExitBucketOption is called when exiting the bucketOption production.
	ExitBucketOption(c *BucketOptionContext)

	// ExitGenericCommand is called when exiting the genericCommand production.
	ExitGenericCommand(c *GenericCommandContext)

	// ExitGenericArg is called when exiting the genericArg production.
	ExitGenericArg(c *GenericArgContext)

	// ExitSubsearch is called when exiting the subsearch production.
	ExitSubsearch(c *SubsearchContext)

	// ExitSearchExpression is called when exiting the searchExpression production.
	ExitSearchExpression(c *SearchExpressionContext)

	// ExitSearchTerm is called when exiting the searchTerm production.
	ExitSearchTerm(c *SearchTermContext)

	// ExitCondition is called when exiting the condition production.
	ExitCondition(c *ConditionContext)

	// ExitComparisonOp is called when exiting the comparisonOp production.
	ExitComparisonOp(c *ComparisonOpContext)

	// ExitLogicalOp is called when exiting the logicalOp production.
	ExitLogicalOp(c *LogicalOpContext)

	// ExitExpression is called when exiting the expression production.
	ExitExpression(c *ExpressionContext)

	// ExitOrExpression is called when exiting the orExpression production.
	ExitOrExpression(c *OrExpressionContext)

	// ExitAndExpression is called when exiting the andExpression production.
	ExitAndExpression(c *AndExpressionContext)

	// ExitNotExpression is called when exiting the notExpression production.
	ExitNotExpression(c *NotExpressionContext)

	// ExitComparisonExpression is called when exiting the comparisonExpression production.
	ExitComparisonExpression(c *ComparisonExpressionContext)

	// ExitAdditiveExpression is called when exiting the additiveExpression production.
	ExitAdditiveExpression(c *AdditiveExpressionContext)

	// ExitMultiplicativeExpression is called when exiting the multiplicativeExpression production.
	ExitMultiplicativeExpression(c *MultiplicativeExpressionContext)

	// ExitUnaryExpression is called when exiting the unaryExpression production.
	ExitUnaryExpression(c *UnaryExpressionContext)

	// ExitPrimaryExpression is called when exiting the primaryExpression production.
	ExitPrimaryExpression(c *PrimaryExpressionContext)

	// ExitFunctionCall is called when exiting the functionCall production.
	ExitFunctionCall(c *FunctionCallContext)

	// ExitArgumentList is called when exiting the argumentList production.
	ExitArgumentList(c *ArgumentListContext)

	// ExitValue is called when exiting the value production.
	ExitValue(c *ValueContext)

	// ExitColonValue is called when exiting the colonValue production.
	ExitColonValue(c *ColonValueContext)

	// ExitWildcardValue is called when exiting the wildcardValue production.
	ExitWildcardValue(c *WildcardValueContext)

	// ExitBareWord is called when exiting the bareWord production.
	ExitBareWord(c *BareWordContext)

	// ExitFieldName is called when exiting the fieldName production.
	ExitFieldName(c *FieldNameContext)

	// ExitFieldList is called when exiting the fieldList production.
	ExitFieldList(c *FieldListContext)

	// ExitFieldOrQuoted is called when exiting the fieldOrQuoted production.
	ExitFieldOrQuoted(c *FieldOrQuotedContext)

	// ExitValueList is called when exiting the valueList production.
	ExitValueList(c *ValueListContext)
}
