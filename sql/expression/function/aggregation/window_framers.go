// Copyright 2022 DoltHub, Inc.
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

package aggregation

import (
	"errors"
	"io"

	"github.com/dolthub/go-mysql-server/sql"
)

var ErrPartitionNotSet = errors.New("attempted to general a window frame interval before framer partition was set")

var _ sql.WindowFramer = (*RowFramer)(nil)
var _ sql.WindowFramer = (*PartitionFramer)(nil)
var _ sql.WindowFramer = (*GroupByFramer)(nil)

func NewUnboundedPrecedingToCurrentRowFramer() *RowFramer {
	return &RowFramer{
		unboundedPreceding: true,
		followingOffset:    0,
		frameEnd:           -1,
		frameStart:         -1,
		partitionStart:     -1,
		partitionEnd:       -1,
	}
}

type RowFramer struct {
	idx                          int
	partitionStart, partitionEnd int
	frameStart, frameEnd         int
	partitionSet                 bool

	followingOffset, precedingOffset       int
	unboundedPreceding, unboundedFollowing bool
}

func (f *RowFramer) Close() {
	panic("implement me")
}

func (f *RowFramer) NewFramer(interval sql.WindowInterval) sql.WindowFramer {
	return &RowFramer{
		idx:            interval.Start,
		partitionStart: interval.Start,
		partitionEnd:   interval.End,
		frameStart:     -1,
		frameEnd:       -1,
		partitionSet:   true,
		// pass through parent state
		unboundedPreceding: f.unboundedPreceding,
		unboundedFollowing: f.unboundedFollowing,
		followingOffset:    f.followingOffset,
		precedingOffset:    f.precedingOffset,
	}
}

func (f *RowFramer) Next() (sql.WindowInterval, error) {
	if f.idx != 0 && f.idx >= f.partitionEnd || !f.partitionSet {
		return sql.WindowInterval{}, io.EOF
	}

	newStart := f.idx - f.precedingOffset
	if f.unboundedPreceding || newStart < f.partitionStart {
		newStart = f.partitionStart
	}

	newEnd := f.idx + f.followingOffset + 1
	if f.unboundedFollowing || newEnd > f.partitionEnd {
		newEnd = f.partitionEnd
	}

	f.frameStart = newStart
	f.frameEnd = newEnd

	f.idx++
	return f.Interval()
}

func (f *RowFramer) FirstIdx() int {
	return f.frameEnd
}

func (f *RowFramer) LastIdx() int {
	return f.frameStart
}

func (f *RowFramer) Interval() (sql.WindowInterval, error) {
	if !f.partitionSet {
		return sql.WindowInterval{}, ErrPartitionNotSet
	}
	return sql.WindowInterval{Start: f.frameStart, End: f.frameEnd}, nil
}

func (f *RowFramer) SlidingInterval(ctx sql.Context) (sql.WindowInterval, sql.WindowInterval, sql.WindowInterval) {
	panic("implement me")
}

type PartitionFramer struct {
	idx                          int
	partitionStart, partitionEnd int

	followOffset, precOffset int
	frameStart, frameEnd     int
	partitionSet             bool
}

func NewPartitionFramer() *PartitionFramer {
	return &PartitionFramer{
		idx:            -1,
		frameEnd:       -1,
		frameStart:     -1,
		partitionStart: -1,
		partitionEnd:   -1,
	}
}

func (f *PartitionFramer) NewFramer(interval sql.WindowInterval) sql.WindowFramer {
	return &PartitionFramer{
		idx:            interval.Start,
		frameEnd:       interval.End,
		frameStart:     interval.Start,
		partitionStart: interval.Start,
		partitionEnd:   interval.End,
		partitionSet:   true,
	}
}

func (f *PartitionFramer) Next() (sql.WindowInterval, error) {
	if !f.partitionSet {
		return sql.WindowInterval{}, io.EOF
	}
	if f.idx == 0 || (0 < f.idx && f.idx < f.partitionEnd) {
		f.idx++
		return f.Interval()
	}
	return sql.WindowInterval{}, io.EOF
}

func (f *PartitionFramer) FirstIdx() int {
	return f.frameStart
}

func (f *PartitionFramer) LastIdx() int {
	return f.frameEnd
}

func (f *PartitionFramer) Interval() (sql.WindowInterval, error) {
	if !f.partitionSet {
		return sql.WindowInterval{}, ErrPartitionNotSet
	}
	return sql.WindowInterval{Start: f.frameStart, End: f.frameEnd}, nil
}

func (f *PartitionFramer) SlidingInterval(ctx sql.Context) (sql.WindowInterval, sql.WindowInterval, sql.WindowInterval) {
	panic("implement me")
}

func (f *PartitionFramer) Close() {
	panic("implement me")
}

func NewGroupByFramer() *GroupByFramer {
	return &GroupByFramer{
		frameEnd:       -1,
		frameStart:     -1,
		partitionStart: -1,
		partitionEnd:   -1,
	}
}

type GroupByFramer struct {
	evaluated                    bool
	partitionStart, partitionEnd int

	frameStart, frameEnd int
	partitionSet         bool
}

func (f *GroupByFramer) NewFramer(interval sql.WindowInterval) sql.WindowFramer {
	return &GroupByFramer{
		evaluated:      false,
		frameEnd:       interval.End,
		frameStart:     interval.Start,
		partitionStart: interval.Start,
		partitionEnd:   interval.End,
		partitionSet:   true,
	}
}

func (f *GroupByFramer) Next() (sql.WindowInterval, error) {
	if !f.partitionSet {
		return sql.WindowInterval{}, io.EOF
	}
	if !f.evaluated {
		f.evaluated = true
		return f.Interval()
	}
	return sql.WindowInterval{}, io.EOF
}

func (f *GroupByFramer) FirstIdx() int {
	return f.frameStart
}

func (f *GroupByFramer) LastIdx() int {
	return f.frameEnd
}

func (f *GroupByFramer) Interval() (sql.WindowInterval, error) {
	if !f.partitionSet {
		return sql.WindowInterval{}, ErrPartitionNotSet
	}
	return sql.WindowInterval{Start: f.frameStart, End: f.frameEnd}, nil
}

func (f *GroupByFramer) SlidingInterval(ctx sql.Context) (sql.WindowInterval, sql.WindowInterval, sql.WindowInterval) {
	panic("implement me")
}
