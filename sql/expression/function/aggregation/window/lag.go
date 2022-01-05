// Copyright 2021 Dolthub, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package window

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/src-d/go-errors.v1"

	"github.com/dolthub/go-mysql-server/sql"
	"github.com/dolthub/go-mysql-server/sql/expression"
)

var ErrInvalidLagOffset = errors.NewKind("'LAG' offset must be a non-negative integer; found: %v")
var ErrInvalidLagDefault = errors.NewKind("'LAG' default must be a literal; found: %v")

type Lag struct {
	window *sql.Window
	expression.NaryExpression
	offset int
	pos    int
}

var _ sql.FunctionExpression = (*Lag)(nil)
var _ sql.WindowAggregation = (*Lag)(nil)

// getLagOffset extracts a non-negative integer from an expression.Literal, or errors
func getLagOffset(e sql.Expression) (int, error) {
	lit, ok := e.(*expression.Literal)
	if !ok {
		return 0, ErrInvalidLagOffset.New(e)
	}
	val := lit.Value()
	var offset int
	switch e := val.(type) {
	case int:
		offset = e
	case int8:
		offset = int(e)
	case int16:
		offset = int(e)
	case int32:
		offset = int(e)
	case int64:
		offset = int(e)
	default:
		return 0, ErrInvalidLagOffset.New(e)
	}

	if offset < 0 {
		return 0, ErrInvalidLagOffset.New(e)
	}

	return offset, nil
}

// NewLag accepts variadic arguments to create a new Lag node:
// If 1 expression, use default values for [default] and [offset]
// If 2 expressions, use default value for [default]
// 3 input expression match to [child], [offset], and [default] arguments
// The offset is constrained to a non-negative integer expression.Literal.
// TODO: support user-defined variable offset
func NewLag(e ...sql.Expression) (*Lag, error) {
	switch len(e) {
	case 1:
		return &Lag{NaryExpression: expression.NaryExpression{ChildExpressions: e[:1]}, offset: 1}, nil
	case 2:
		offset, err := getLagOffset(e[1])
		if err != nil {
			return nil, err
		}
		return &Lag{NaryExpression: expression.NaryExpression{ChildExpressions: e[:1]}, offset: offset}, nil
	case 3:
		offset, err := getLagOffset(e[1])
		if err != nil {
			return nil, err
		}
		return &Lag{NaryExpression: expression.NaryExpression{ChildExpressions: []sql.Expression{e[0], e[2]}}, offset: offset}, nil
	}
	return nil, sql.ErrInvalidArgumentNumber.New("LAG", "1, 2, or 3", len(e))
}

// Description implements sql.FunctionExpression
func (l *Lag) Description() string {
	return "returns the value of the expression evaluated at the lag offset row"
}

// Window implements sql.WindowExpression
func (l *Lag) Window() *sql.Window {
	return l.window
}

// IsNullable implements sql.Expression
func (l *Lag) Resolved() bool {
	childrenResolved := true
	for _, c := range l.ChildExpressions {
		childrenResolved = childrenResolved && c.Resolved()
	}
	return childrenResolved && windowResolved(l.window)
}

func (l *Lag) NewBuffer() sql.Row {
	return sql.NewRow(make([]sql.Row, 0))
}

func (l *Lag) String() string {
	sb := strings.Builder{}
	if len(l.ChildExpressions) > 1 {
		sb.WriteString(fmt.Sprintf("lag(%s, %d, %s)", l.ChildExpressions[0].String(), l.offset, l.ChildExpressions[1]))
	} else {
		sb.WriteString(fmt.Sprintf("lag(%s, %d)", l.ChildExpressions[0].String(), l.offset))
	}
	if l.window != nil {
		sb.WriteString(" ")
		sb.WriteString(l.window.String())
	}
	return sb.String()
}

func (l *Lag) DebugString() string {
	sb := strings.Builder{}
	if len(l.ChildExpressions) > 1 {
		sb.WriteString(fmt.Sprintf("lag(%s, %d, %s)", l.ChildExpressions[0].String(), l.offset, l.ChildExpressions[1]))
	} else {
		sb.WriteString(fmt.Sprintf("lag(%s, %d)", l.ChildExpressions[0].String(), l.offset))
	}
	if l.window != nil {
		sb.WriteString(" ")
		sb.WriteString(sql.DebugString(l.window))
	}
	return sb.String()
}

// FunctionName implements sql.FunctionExpression
func (l *Lag) FunctionName() string {
	return "LAG"
}

// Type implements sql.Expression
func (l *Lag) Type() sql.Type {
	return l.ChildExpressions[0].Type()
}

// IsNullable implements sql.Expression
func (l *Lag) IsNullable() bool {
	return true
}

// Eval implements sql.Expression
func (l *Lag) Eval(ctx *sql.Context, row sql.Row) (interface{}, error) {
	panic("eval called on window function")
}

// Children implements sql.Expression
func (l *Lag) Children() []sql.Expression {
	if l == nil {
		return nil
	}
	return append(l.window.ToExpressions(), l.ChildExpressions...)
}

// WithChildren implements sql.Expression
func (l *Lag) WithChildren(children ...sql.Expression) (sql.Expression, error) {
	if len(children) < 2 {
		return nil, sql.ErrInvalidChildrenNumber.New(l, len(children), 2)
	}

	nl := *l
	numWindowExpr := len(children) - len(l.ChildExpressions)
	window, err := l.window.FromExpressions(children[:numWindowExpr])
	if err != nil {
		return nil, err
	}

	nl.ChildExpressions = children[numWindowExpr:]
	nl.window = window

	return &nl, nil
}

// WithWindow implements sql.WindowAggregation
func (l *Lag) WithWindow(window *sql.Window) (sql.WindowAggregation, error) {
	nl := *l
	nl.window = window
	return &nl, nil
}

// Add implements sql.WindowAggregation
func (l *Lag) Add(ctx *sql.Context, buffer, row sql.Row) error {
	rows := buffer[0].([]sql.Row)
	// order -> row, original_idx
	buffer[0] = append(rows, append(row, nil, l.pos))

	l.pos++
	return nil
}

// Finish implements sql.WindowAggregation
func (l *Lag) Finish(ctx *sql.Context, buffer sql.Row) error {
	rows := buffer[0].([]sql.Row)
	if len(rows) > 0 && l.window != nil && l.window.OrderBy != nil {
		sorter := &expression.Sorter{
			SortFields: append(partitionsToSortFields(l.Window().PartitionBy), l.Window().OrderBy...),
			Rows:       rows,
			Ctx:        ctx,
		}
		sort.Stable(sorter)
		if sorter.LastError != nil {
			return sorter.LastError
		}

		// Now that we have the rows in sorted order, set the lag expression
		lagIdx := len(rows[0]) - 2
		originalIdx := len(rows[0]) - 1
		var last sql.Row
		var err error
		var isNew bool
		var partIdx int
		for i, row := range rows {
			// every time we encounter a new partition, reset the partIdx for lag reference
			isNew, err = isNewPartition(ctx, l.window.PartitionBy, last, row)
			if err != nil {
				return err
			}
			if isNew {
				partIdx = 0
			}

			if partIdx >= l.offset {
				row[lagIdx], err = l.ChildExpressions[0].Eval(ctx, rows[i-l.offset])
				if err != nil {
					return nil
				}
			} else if len(l.ChildExpressions) > 1 {
				row[lagIdx], err = l.ChildExpressions[1].Eval(ctx, row)
			}
			partIdx++
			last = row
		}

		// And finally sort again by the original order
		sort.SliceStable(rows, func(i, j int) bool {
			return rows[i][originalIdx].(int) < rows[j][originalIdx].(int)
		})
	}
	return nil
}

// EvalRow implements sql.WindowAggregation
func (l *Lag) EvalRow(i int, buffer sql.Row) (interface{}, error) {
	rows := buffer[0].([]sql.Row)
	lagIdx := len(rows[0]) - 2
	return rows[i][lagIdx], nil
}
