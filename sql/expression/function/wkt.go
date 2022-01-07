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

package function

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dolthub/go-mysql-server/sql"
	"github.com/dolthub/go-mysql-server/sql/expression"
)

// AsWKT is a function that converts a spatial type into WKT format (alias for AsText)
type AsWKT struct {
	expression.UnaryExpression
}

var _ sql.FunctionExpression = (*AsWKT)(nil)

// NewAsWKT creates a new point expression.
func NewAsWKT(e sql.Expression) sql.Expression {
	return &AsWKT{expression.UnaryExpression{Child: e}}
}

// FunctionName implements sql.FunctionExpression
func (p *AsWKT) FunctionName() string {
	return "st_aswkb"
}

// Description implements sql.FunctionExpression
func (p *AsWKT) Description() string {
	return "returns binary representation of given spatial type."
}

// IsNullable implements the sql.Expression interface.
func (p *AsWKT) IsNullable() bool {
	return p.Child.IsNullable()
}

// Type implements the sql.Expression interface.
func (p *AsWKT) Type() sql.Type {
	return p.Child.Type()
}

func (p *AsWKT) String() string {
	return fmt.Sprintf("ST_ASWKT(%s)", p.Child.String())
}

// WithChildren implements the Expression interface.
func (p *AsWKT) WithChildren(children ...sql.Expression) (sql.Expression, error) {
	if len(children) != 1 {
		return nil, sql.ErrInvalidChildrenNumber.New(p, len(children), 1)
	}
	return NewAsWKT(children[0]), nil
}

// PointToWKT converts a sql.Point to a byte array
func PointToWKT(p sql.Point) string {
	x := strconv.FormatFloat(p.X, 'g', -1, 64)
	y := strconv.FormatFloat(p.Y, 'g', -1, 64)
	return fmt.Sprintf("%s %s", x, y)
}

// LineToWKT converts a sql.Linestring to a byte array
func LineToWKT(l sql.Linestring) string {
	points := make([]string, len(l.Points))
	for i, p := range l.Points {
		points[i] = PointToWKT(p)
	}
	return strings.Join(points, ",")
}

// PolygonToWKT converts a sql.Polygon to a byte array
func PolygonToWKT(p sql.Polygon) string {
	lines := make([]string, len(p.Lines))
	for i, l := range p.Lines {
		lines[i] = "(" + LineToWKT(l) + ")"
	}
	return strings.Join(lines, ",")
}

// Eval implements the sql.Expression interface.
func (p *AsWKT) Eval(ctx *sql.Context, row sql.Row) (interface{}, error) {
	// Evaluate child
	val, err := p.Child.Eval(ctx, row)
	if err != nil {
		return nil, err
	}

	if val == nil {
		return nil, nil
	}

	var geomType string
	var data string
	// Expect one of the geometry types
	switch v := val.(type) {
	case sql.Point:
		// Mark as point type
		geomType = "POINT"
		data = PointToWKT(v)
	case sql.Linestring:
		// Mark as linestring type
		geomType = "LINESTRING"
		data = LineToWKT(v)
	case sql.Polygon:
		// Mark as Polygon type
		geomType = "POLYGON"
		data = PolygonToWKT(v)
	default:
		return nil, sql.ErrInvalidGISData.New("ST_AsWKT")
	}

	return fmt.Sprintf("%s(%s)", geomType, data), nil
}

// GeomFromText is a function that returns a point type from a WKT string
type GeomFromText struct {
	expression.UnaryExpression
}

var _ sql.FunctionExpression = (*GeomFromText)(nil)

// NewGeomFromWKT creates a new point expression.
func NewGeomFromWKT(e sql.Expression) sql.Expression {
	return &GeomFromText{expression.UnaryExpression{Child: e}}
}

// FunctionName implements sql.FunctionExpression
func (p *GeomFromText) FunctionName() string {
	return "st_geomfromwkt"
}

// Description implements sql.FunctionExpression
func (p *GeomFromText) Description() string {
	return "returns a new point from a WKT string."
}

// IsNullable implements the sql.Expression interface.
func (p *GeomFromText) IsNullable() bool {
	return p.Child.IsNullable()
}

// Type implements the sql.Expression interface.
func (p *GeomFromText) Type() sql.Type {
	return p.Child.Type()
}

func (p *GeomFromText) String() string {
	return fmt.Sprintf("ST_GEOMFROMWKT(%s)", p.Child.String())
}

// WithChildren implements the Expression interface.
func (p *GeomFromText) WithChildren(children ...sql.Expression) (sql.Expression, error) {
	if len(children) != 1 {
		return nil, sql.ErrInvalidChildrenNumber.New(p, len(children), 1)
	}
	return NewGeomFromWKT(children[0]), nil
}

// ParseWKTHeader should extract the type from the geometry string
func ParseWKTHeader(s string) (string, string, error) {
	// Read until first open parenthesis
	end := strings.Index(s, "(")

	// Bad if no parenthesis found
	if end == -1 {
		return "", "", sql.ErrInvalidGISData.New("ST_GeomFromText")
	}

	// Get Geometry Type
	geomType := s[:end]
	geomType = strings.TrimSpace(geomType)
	geomType = strings.ToLower(geomType)

	// Get data
	data := s[end:]
	data = strings.TrimSpace(data)

	// Check that data is surrounded by parentheses
	if data[0] != '(' || data[len(data)-1] != ')' {
		return "", "", sql.ErrInvalidGISData.New("ST_GeomFromText")
	}
	// Remove parentheses, and trim
	data = data[1 : len(data)-1]
	data = strings.TrimSpace(data)

	return geomType, data, nil
}

// WKTToPoint expects a string like this "1.2 3.4"
func WKTToPoint(s string) (sql.Point, error) {
	// Empty string is wrong
	if len(s) == 0 {
		return sql.Point{}, sql.ErrInvalidGISData.New("ST_PointFromText")
	}

	// Get everything between spaces
	args := strings.Fields(s)

	// Check length
	if len(args) != 2 {
		return sql.Point{}, sql.ErrInvalidGISData.New("ST_PointFromText")
	}

	// Parse x
	x, err := strconv.ParseFloat(args[0], 64)
	if err != nil {
		return sql.Point{}, sql.ErrInvalidGISData.New("ST_PointFromText")
	}

	// Parse y
	y, err := strconv.ParseFloat(args[1], 64)
	if err != nil {
		return sql.Point{}, sql.ErrInvalidGISData.New("ST_PointFromText")
	}

	// Create point object
	return sql.Point{X: x, Y: y}, nil
}

// WKTToLine expects a string like "1.2 3.4, 5.6 7.8, ..."
func WKTToLine(s string) (sql.Linestring, error) {
	// Empty string is wrong
	if len(s) == 0 {
		return sql.Linestring{}, sql.ErrInvalidGISData.New("ST_LineFromText")
	}

	// Separate by comma
	pointStrs := strings.Split(s, ",")

	// Parse each point string
	var points = make([]sql.Point, len(pointStrs))
	for i, ps := range pointStrs {
		// Remove leading and trailing whitespace
		ps = strings.TrimSpace(ps)

		// Parse point
		if p, err := WKTToPoint(ps); err == nil {
			points[i] = p
		} else {
			return sql.Linestring{}, sql.ErrInvalidGISData.New("ST_LineFromText")
		}
	}

	// Create Linestring object
	return sql.Linestring{Points: points}, nil
}

// WKTToPoly Expects a string like "(1 2, 3 4), (5 6, 7 8), ..."
func WKTToPoly(s string) (sql.Polygon, error) {
	var lines []sql.Linestring
	for {
		// Look for closing parentheses
		end := strings.Index(s, ")")
		if end == -1 {
			return sql.Polygon{}, sql.ErrInvalidGISData.New("ST_PolyFromText")
		}

		// Extract linestring string; does not include ")"
		lineStr := s[:end]

		// Must start with open parenthesis
		if len(lineStr) == 0 || lineStr[0] != '(' {
			return sql.Polygon{}, sql.ErrInvalidGISData.New("ST_PolyFromText")
		}

		// Remove leading "("
		lineStr = lineStr[1:]

		// Remove leading and trailing whitespace
		lineStr = strings.TrimSpace(lineStr)

		// Parse line
		if line, err := WKTToLine(lineStr); err == nil {
			// Check if line is linearring
			if isLinearRing(line) {
				lines = append(lines, line)
			} else {
				return sql.Polygon{}, sql.ErrInvalidGISData.New("ST_PolyFromText")
			}
		} else {
			return sql.Polygon{}, sql.ErrInvalidGISData.New("ST_PolyFromText")
		}

		// Prepare next string
		s = s[end+1:]
		s = strings.TrimSpace(s)

		// Reached end
		if len(s) == 0 {
			break
		}

		// Linestrings must be comma-separated
		if s[0] != ',' {
			return sql.Polygon{}, sql.ErrInvalidGISData.New("ST_PolyFromText")
		}

		// Drop leading comma
		s = s[1:]

		// Trim leading spaces
		s = strings.TrimSpace(s)
	}

	// Create Polygon object
	return sql.Polygon{Lines: lines}, nil
}

// Eval implements the sql.Expression interface.
func (p *GeomFromText) Eval(ctx *sql.Context, row sql.Row) (interface{}, error) {
	// Evaluate child
	val, err := p.Child.Eval(ctx, row)
	if err != nil {
		return nil, err
	}

	if val == nil {
		return nil, nil
	}

	// Expect a string, throw error otherwise
	s, ok := val.(string)
	if !ok {
		return nil, sql.ErrInvalidGISData.New("ST_GeomFromText")
	}

	// Determine type, and get data
	geomType, data, err := ParseWKTHeader(s)
	if err != nil {
		return nil, err
	}

	// Parse accordingly
	// TODO: define consts instead of string comparison?
	switch geomType {
	case "point":
		return WKTToPoint(data)
	case "linestring":
		return WKTToLine(data)
	case "polygon":
		return WKTToPoly(data)
	default:
		return nil, sql.ErrInvalidGISData.New("ST_GeomFromText")
	}
}

// PointFromWKT is a function that returns a point type from a WKT string
type PointFromWKT struct {
	expression.UnaryExpression
}

var _ sql.FunctionExpression = (*PointFromWKT)(nil)

// NewPointFromWKT creates a new point expression.
func NewPointFromWKT(e sql.Expression) sql.Expression {
	return &PointFromWKT{expression.UnaryExpression{Child: e}}
}

// FunctionName implements sql.FunctionExpression
func (p *PointFromWKT) FunctionName() string {
	return "st_pointfromwkt"
}

// Description implements sql.FunctionExpression
func (p *PointFromWKT) Description() string {
	return "returns a new point from a WKT string."
}

// IsNullable implements the sql.Expression interface.
func (p *PointFromWKT) IsNullable() bool {
	return p.Child.IsNullable()
}

// Type implements the sql.Expression interface.
func (p *PointFromWKT) Type() sql.Type {
	return p.Child.Type()
}

func (p *PointFromWKT) String() string {
	return fmt.Sprintf("ST_POINTFROMWKT(%s)", p.Child.String())
}

// WithChildren implements the Expression interface.
func (p *PointFromWKT) WithChildren(children ...sql.Expression) (sql.Expression, error) {
	if len(children) != 1 {
		return nil, sql.ErrInvalidChildrenNumber.New(p, len(children), 1)
	}
	return NewPointFromWKT(children[0]), nil
}

// Eval implements the sql.Expression interface.
func (p *PointFromWKT) Eval(ctx *sql.Context, row sql.Row) (interface{}, error) {
	// Evaluate child
	val, err := p.Child.Eval(ctx, row)
	if err != nil {
		return nil, err
	}

	if val == nil {
		return nil, nil
	}

	// Expect a string, throw error otherwise
	if s, ok := val.(string); ok {
		if geomType, data, err := ParseWKTHeader(s); err == nil && geomType == "point" {
			return WKTToPoint(data)
		}
	}

	return nil, sql.ErrInvalidGISData.New("ST_PointFromText")
}

// LineFromWKT is a function that returns a point type from a WKT string
type LineFromWKT struct {
	expression.UnaryExpression
}

var _ sql.FunctionExpression = (*LineFromWKT)(nil)

// NewLineFromWKT creates a new point expression.
func NewLineFromWKT(e sql.Expression) sql.Expression {
	return &LineFromWKT{expression.UnaryExpression{Child: e}}
}

// FunctionName implements sql.FunctionExpression
func (p *LineFromWKT) FunctionName() string {
	return "st_linefromwkt"
}

// Description implements sql.FunctionExpression
func (p *LineFromWKT) Description() string {
	return "returns a new line from a WKT string."
}

// IsNullable implements the sql.Expression interface.
func (p *LineFromWKT) IsNullable() bool {
	return p.Child.IsNullable()
}

// Type implements the sql.Expression interface.
func (p *LineFromWKT) Type() sql.Type {
	return p.Child.Type()
}

func (p *LineFromWKT) String() string {
	return fmt.Sprintf("ST_LINEFROMWKT(%s)", p.Child.String())
}

// WithChildren implements the Expression interface.
func (p *LineFromWKT) WithChildren(children ...sql.Expression) (sql.Expression, error) {
	if len(children) != 1 {
		return nil, sql.ErrInvalidChildrenNumber.New(p, len(children), 1)
	}
	return NewLineFromWKT(children[0]), nil
}

// Eval implements the sql.Expression interface.
func (p *LineFromWKT) Eval(ctx *sql.Context, row sql.Row) (interface{}, error) {
	// Evaluate child
	val, err := p.Child.Eval(ctx, row)
	if err != nil {
		return nil, err
	}

	if val == nil {
		return nil, nil
	}

	// Expect a string, throw error otherwise
	if s, ok := val.(string); ok {
		if geomType, data, err := ParseWKTHeader(s); err == nil && geomType == "linestring" {
			return WKTToLine(data)
		}
	}

	return nil, sql.ErrInvalidGISData.New("ST_LineFromText")
}

// PolyFromWKT is a function that returns a polygon type from a WKT string
type PolyFromWKT struct {
	expression.UnaryExpression
}

var _ sql.FunctionExpression = (*PolyFromWKT)(nil)

// NewPolyFromWKT creates a new polygon expression.
func NewPolyFromWKT(e sql.Expression) sql.Expression {
	return &PolyFromWKT{expression.UnaryExpression{Child: e}}
}

// FunctionName implements sql.FunctionExpression
func (p *PolyFromWKT) FunctionName() string {
	return "st_polyfromwkt"
}

// Description implements sql.FunctionExpression
func (p *PolyFromWKT) Description() string {
	return "returns a new polygon from a WKT string."
}

// IsNullable implements the sql.Expression interface.
func (p *PolyFromWKT) IsNullable() bool {
	return p.Child.IsNullable()
}

// Type implements the sql.Expression interface.
func (p *PolyFromWKT) Type() sql.Type {
	return p.Child.Type()
}

func (p *PolyFromWKT) String() string {
	return fmt.Sprintf("ST_POLYFROMWKT(%s)", p.Child.String())
}

// WithChildren implements the Expression interface.
func (p *PolyFromWKT) WithChildren(children ...sql.Expression) (sql.Expression, error) {
	if len(children) != 1 {
		return nil, sql.ErrInvalidChildrenNumber.New(p, len(children), 1)
	}
	return NewPolyFromWKT(children[0]), nil
}

// Eval implements the sql.Expression interface.
func (p *PolyFromWKT) Eval(ctx *sql.Context, row sql.Row) (interface{}, error) {
	// Evaluate child
	val, err := p.Child.Eval(ctx, row)
	if err != nil {
		return nil, err
	}

	if val == nil {
		return nil, nil
	}

	// Expect a string, throw error otherwise
	if s, ok := val.(string); ok {
		// TODO: possible to use a regular expression? "*polygon *\( *[0-9][0-9]* *[0-9][0-9]* *\) *" /gi
		if geomType, data, err := ParseWKTHeader(s); err == nil && geomType == "polygon" {
			return WKTToPoly(data)
		}
	}

	return nil, sql.ErrInvalidGISData.New("ST_PolyFromText")
}
