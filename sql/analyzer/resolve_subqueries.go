// Copyright 2020-2021 Dolthub, Inc.
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

package analyzer

import (
	"github.com/dolthub/go-mysql-server/sql"
	"github.com/dolthub/go-mysql-server/sql/expression"
	"github.com/dolthub/go-mysql-server/sql/plan"
	"github.com/dolthub/go-mysql-server/sql/visit"
)

func resolveSubqueries(ctx *sql.Context, a *Analyzer, n sql.Node, scope *Scope) (sql.Node, sql.TreeIdentity, error) {
	span, ctx := ctx.Span("resolve_subqueries")
	defer span.Finish()

	return visit.Nodes(n, func(n sql.Node) (sql.Node, sql.TreeIdentity, error) {
		switch n := n.(type) {
		case *plan.SubqueryAlias:
			// subqueries do not have access to outer scope
			child, same, err := a.analyzeThroughBatch(ctx, n.Child, nil, "default-rules")
			if err != nil {
				return nil, sql.SameTree, err
			}

			if len(n.Columns) > 0 {
				schemaLen := schemaLength(n.Child)
				if schemaLen != len(n.Columns) {
					return nil, sql.SameTree, sql.ErrColumnCountMismatch.New()
				}
			}
			if same {
				return n, sql.SameTree, nil
			}
			newn, err := n.WithChildren(StripPassthroughNodes(child))
			return newn, sql.NewTree, err
		default:
			return n, sql.SameTree, nil
		}
	})
}

func finalizeSubqueries(ctx *sql.Context, a *Analyzer, n sql.Node, scope *Scope) (sql.Node, sql.TreeIdentity, error) {
	span, ctx := ctx.Span("finalize_subqueries")
	defer span.Finish()

	return visit.Nodes(n, func(n sql.Node) (sql.Node, sql.TreeIdentity, error) {
		switch n := n.(type) {
		case *plan.SubqueryAlias:
			// subqueries do not have access to outer scope
			child, same, err := a.analyzeStartingAtBatch(ctx, n.Child, nil, "default-rules")
			if err != nil {
				return nil, sql.SameTree, err
			}

			if len(n.Columns) > 0 {
				schemaLen := schemaLength(n.Child)
				if schemaLen != len(n.Columns) {
					return nil, sql.SameTree, sql.ErrColumnCountMismatch.New()
				}
			}
			if same {
				return n, sql.SameTree, nil
			}
			newn, err := n.WithChildren(StripPassthroughNodes(child))
			return newn, sql.NewTree, err
		default:
			return n, sql.SameTree, nil
		}
	})
}

func flattenTableAliases(ctx *sql.Context, a *Analyzer, n sql.Node, scope *Scope) (sql.Node, sql.TreeIdentity, error) {
	span, ctx := ctx.Span("flatten_table_aliases")
	defer span.Finish()
	return visit.Nodes(n, func(n sql.Node) (sql.Node, sql.TreeIdentity, error) {
		switch n := n.(type) {
		case *plan.TableAlias:
			if sa, isSA := n.Children()[0].(*plan.SubqueryAlias); isSA {
				return sa.WithName(n.Name()), sql.NewTree, nil
			}
			if ta, isTA := n.Children()[0].(*plan.TableAlias); isTA {
				return ta.WithName(n.Name()), sql.NewTree, nil
			}
			return n, sql.SameTree, nil
		default:
			return n, sql.SameTree, nil
		}
	})
}

func resolveSubqueryExpressions(ctx *sql.Context, a *Analyzer, n sql.Node, scope *Scope) (sql.Node, sql.TreeIdentity, error) {
	newn, _, err := visit.NodesExprsWithNode(n, func(n sql.Node, e sql.Expression) (sql.Expression, sql.TreeIdentity, error) {
		s, ok := e.(*plan.Subquery)
		// We always analyze subquery expressions even if they are resolved, since other transformations to the surrounding
		// query might cause them to need to shift their field indexes.
		if !ok {
			return e, sql.SameTree, nil
		}

		subqueryCtx, cancelFunc := ctx.NewSubContext()
		defer cancelFunc()
		subScope := scope.newScope(n)

		analyzed, same, err := a.analyzeWithSelector(subqueryCtx, s.Query, subScope, analyzeAll)
		if err != nil {
			// We ignore certain errors, deferring them to later analysis passes. Specifically, if the subquery isn't
			// resolved or a column can't be found in the scope node, wait until a later pass.
			// TODO: we won't be able to give the right error message in all cases when we do this, although we attempt to
			//  recover the actual error in the validation step.
			if ErrValidationResolved.Is(err) || sql.ErrTableColumnNotFound.Is(err) || sql.ErrColumnNotFound.Is(err) {
				// keep the work we have and defer remainder of analysis of this subquery until a later pass
				return s.WithQuery(analyzed), same, nil
			}
			return nil, sql.SameTree, err
		}

		if same {
			return e, sql.SameTree, nil
		}
		return s.WithQuery(StripPassthroughNodes(analyzed)), sql.NewTree, nil
	})
	if err != nil {
		return nil, sql.SameTree, err
	}
	if nodesEqual(newn, n) {
		return n, sql.SameTree, nil
	}
	return newn, sql.NewTree, nil
}

// StripPassthroughNodes strips all top-level passthrough nodes meant to apply only to top-level queries (query
// tracking, transaction logic, etc) from the node tree given and return the first non-passthrough child element. This
// is useful for when we invoke the analyzer recursively when e.g. analyzing subqueries or triggers
// TODO: instead of stripping this node off after analysis, it would be better to just not add it in the first place.
func StripPassthroughNodes(n sql.Node) sql.Node {
	nodeIsPassthrough := true
	for nodeIsPassthrough {
		switch tn := n.(type) {
		case *plan.QueryProcess:
			n = tn.Child
		case *plan.StartTransaction:
			n = tn.Child
		default:
			nodeIsPassthrough = false
		}
	}

	return n
}

func exprIsCacheable(expr sql.Expression, lowestAllowedIdx int) bool {
	cacheable := true
	sql.Inspect(expr, func(e sql.Expression) bool {
		if gf, ok := e.(*expression.GetField); ok {
			if gf.Index() < lowestAllowedIdx {
				cacheable = false
				return false
			}
		}
		if nd, ok := e.(sql.NonDeterministicExpression); ok && nd.IsNonDeterministic() {
			cacheable = false
			return false
		}
		return true
	})
	return cacheable
}

func nodeIsCacheable(n sql.Node, lowestAllowedIdx int) bool {
	cacheable := true
	visit.Inspect(n, func(node sql.Node) bool {
		if er, ok := node.(sql.Expressioner); ok {
			for _, expr := range er.Expressions() {
				if !exprIsCacheable(expr, lowestAllowedIdx) {
					cacheable = false
					return false
				}
			}
		} else if _, ok := node.(*plan.SubqueryAlias); ok {
			// SubqueryAliases are always cacheable.  In fact, we
			// do not go far enough here yet. CTEs must be cached /
			// materialized and the same result set used throughout
			// the query when they are non-determinstic in order to
			// give correct results.
			return false
		}
		return true
	})
	return cacheable
}

func isDeterminstic(n sql.Node) bool {
	res := true
	visit.InspectExpressions(n, func(e sql.Expression) bool {
		if s, ok := e.(*plan.Subquery); ok {
			if !isDeterminstic(s.Query) {
				res = false
			}
			return false
		} else if nd, ok := e.(sql.NonDeterministicExpression); ok && nd.IsNonDeterministic() {
			res = false
			return false
		}
		return true
	})
	return res
}

// cacheSubqueryResults determines whether it's safe to cache the results for any subquery expressions, and marks the
// subquery as cacheable if so. Caching subquery results is safe in the case that no outer scope columns are referenced,
// if all expressions in the subquery are deterministic, and if the subquery isn't inside a trigger block.
func cacheSubqueryResults(ctx *sql.Context, a *Analyzer, n sql.Node, scope *Scope) (sql.Node, sql.TreeIdentity, error) {
	// No need to inspect for trigger blocks as the Analyzer is recursively invoked on trigger blocks.
	if n, ok := n.(*plan.TriggerBeginEndBlock); ok {
		return n, sql.SameTree, nil
	}
	return visit.NodesExprsWithNode(n, func(n sql.Node, e sql.Expression) (sql.Expression, sql.TreeIdentity, error) {
		s, ok := e.(*plan.Subquery)
		if !ok || !s.Resolved() {
			return e, sql.SameTree, nil
		}

		scopeLen := len(scope.newScope(n).Schema())
		cacheable := nodeIsCacheable(s.Query, scopeLen)

		if cacheable {
			return s.WithCachedResults(), sql.NewTree, nil
		}

		return s, sql.SameTree, nil
	})
}

// cacheSubqueryAlisesInJoins will look for joins against subquery aliases that
// will repeatedly execute the subquery, and will insert a *plan.CachedResults
// node on top of those nodes.
func cacheSubqueryAlisesInJoins(ctx *sql.Context, a *Analyzer, n sql.Node, scope *Scope) (sql.Node, sql.TreeIdentity, error) {
	n, sameA, err := visit.NodesWithCtx(n, nil, func(c visit.TransformContext) (sql.Node, sql.TreeIdentity, error) {
		_, isJoin := c.Parent.(plan.JoinNode)
		_, isIndexedJoin := c.Parent.(*plan.IndexedJoin)
		if isJoin || isIndexedJoin {
			_, isSubqueryAlias := c.Node.(*plan.SubqueryAlias)
			if isSubqueryAlias {
				// SubqueryAliases are always cacheable. They
				// cannot reference their outside scope and
				// even when they have non-determinstic
				// expressions they should return the same
				// results across multiple iterations.
				return plan.NewCachedResults(c.Node), sql.NewTree, nil
			}
		}
		return c.Node, sql.SameTree, nil
	})
	if err != nil {
		return n, sql.SameTree, err
	}

	// If the most primary table in the top level join is a CachedResults, remove it.
	// We only want to do this if we're at the top of the tree.
	// TODO: Not a perfect indicator of whether we're at the top of the tree...
	sameD := sql.SameTree
	if scope == nil {
		selector := func(c visit.TransformContext) bool {
			if _, isIndexedJoin := c.Parent.(*plan.IndexedJoin); isIndexedJoin {
				return c.ChildNum == 0
			} else if j, isJoin := c.Parent.(plan.JoinNode); isJoin {
				if j.JoinType() == plan.JoinTypeRight {
					return c.ChildNum == 1
				} else {
					return c.ChildNum == 0
				}
			}
			return true
		}
		n, sameD, err = visit.NodesWithCtx(n, selector, func(c visit.TransformContext) (sql.Node, sql.TreeIdentity, error) {
			cr, isCR := c.Node.(*plan.CachedResults)
			if isCR {
				return cr.UnaryNode.Child, sql.NewTree, nil
			}
			return c.Node, sql.SameTree, nil
		})
	}
	return n, sameA && sameD, err
}

func setJoinScopeLen(ctx *sql.Context, a *Analyzer, n sql.Node, scope *Scope) (sql.Node, sql.TreeIdentity, error) {
	scopeLen := len(scope.Schema())
	if scopeLen == 0 {
		return n, sql.SameTree, nil
	}
	return visit.Nodes(n, func(n sql.Node) (sql.Node, sql.TreeIdentity, error) {
		if j, ok := n.(plan.JoinNode); ok {
			nj := j.WithScopeLen(scopeLen)
			if _, ok := nj.Left().(*plan.StripRowNode); !ok {
				nj, err := nj.WithChildren(
					plan.NewStripRowNode(nj.Left(), scopeLen),
					plan.NewStripRowNode(nj.Right(), scopeLen),
				)
				if err != nil {
					return nil, sql.SameTree, err
				}
				return nj, sql.NewTree, nil
			} else {
				return nj, sql.SameTree, nil
			}
		}
		return n, sql.SameTree, nil
	})
}
