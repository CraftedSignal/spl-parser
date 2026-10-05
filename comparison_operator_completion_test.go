package spl

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"
)

func TestSPLComparisonOperatorDefensiveFallbackEdges(t *testing.T) {
	t.Run("missing field stop returns parsed operator", func(t *testing.T) {
		ctx := newSPLConditionContextForTest("", "=", "alice")
		ctx.FieldName().SetStart(nil)
		ctx.FieldName().SetStop(nil)

		got := (&conditionExtractor{originalQuery: `user="alice"`}).extractComparisonOperator(ctx)
		if got != "=" {
			t.Fatalf("operator with missing field token bounds = %q, want =", got)
		}
	})

	t.Run("value start at operator is tolerated", func(t *testing.T) {
		ctx := newSPLConditionContextForTest("user", "=", "alice")
		ctx.Value().SetStart(newSPLTestToken(SPLParserIDENTIFIER, "alice", 4, 8))
		ctx.Value().SetStop(newSPLTestToken(SPLParserIDENTIFIER, "alice", 4, 8))

		got := (&conditionExtractor{originalQuery: `user=alice`}).extractComparisonOperator(ctx)
		if got != "=" {
			t.Fatalf("operator with value start at equality = %q, want =", got)
		}
	})

	t.Run("fallback detects spaced double equals without value stop", func(t *testing.T) {
		ctx := newSPLConditionContextForTest("user", "=", "alice")
		ctx.Value().SetStart(newSPLTestToken(SPLParserIDENTIFIER, "alice", 6, 10))
		ctx.Value().SetStop(nil)

		got := (&conditionExtractor{originalQuery: `user ==alice`}).extractComparisonOperator(ctx)
		if got != "==" {
			t.Fatalf("fallback operator = %q, want ==", got)
		}
	})
}

func newSPLConditionContextForTest(field, op, value string) *ConditionContext {
	ctx := NewEmptyConditionContext()
	fieldCtx := NewEmptyFieldNameContext()
	opCtx := NewEmptyComparisonOpContext()
	valueCtx := NewEmptyValueContext()

	if field != "" {
		token := newSPLTestToken(SPLParserIDENTIFIER, field, 0, len(field)-1)
		fieldCtx.SetStart(token)
		fieldCtx.SetStop(token)
		fieldCtx.AddTokenNode(token)
	}

	opStart := len(field)
	opToken := newSPLTestToken(SPLParserEQ, op, opStart, opStart+len(op)-1)
	opCtx.SetStart(opToken)
	opCtx.SetStop(opToken)
	opCtx.AddTokenNode(opToken)

	valueStart := opStart + len(op)
	valueToken := newSPLTestToken(SPLParserIDENTIFIER, value, valueStart, valueStart+len(value)-1)
	valueCtx.SetStart(valueToken)
	valueCtx.SetStop(valueToken)
	valueCtx.AddTokenNode(valueToken)

	ctx.AddChild(fieldCtx)
	ctx.AddChild(opCtx)
	ctx.AddChild(valueCtx)
	return ctx
}

type splTestToken struct {
	tokenType  int
	text       string
	start      int
	stop       int
	tokenIndex int
}

func newSPLTestToken(tokenType int, text string, start, stop int) antlr.Token {
	return &splTestToken{
		tokenType:  tokenType,
		text:       text,
		start:      start,
		stop:       stop,
		tokenIndex: -1,
	}
}

func (t *splTestToken) GetSource() *antlr.TokenSourceCharStreamPair { return nil }
func (t *splTestToken) GetTokenType() int                           { return t.tokenType }
func (t *splTestToken) GetChannel() int                             { return antlr.TokenDefaultChannel }
func (t *splTestToken) GetStart() int                               { return t.start }
func (t *splTestToken) GetStop() int                                { return t.stop }
func (t *splTestToken) GetLine() int                                { return 0 }
func (t *splTestToken) GetColumn() int                              { return 0 }
func (t *splTestToken) GetText() string                             { return t.text }
func (t *splTestToken) SetText(text string)                         { t.text = text }
func (t *splTestToken) GetTokenIndex() int                          { return t.tokenIndex }
func (t *splTestToken) SetTokenIndex(index int)                     { t.tokenIndex = index }
func (t *splTestToken) GetTokenSource() antlr.TokenSource           { return nil }
func (t *splTestToken) GetInputStream() antlr.CharStream            { return nil }
func (t *splTestToken) String() string                              { return t.text }
