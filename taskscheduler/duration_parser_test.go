// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package taskscheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDurationScheduleParser(t *testing.T) {
	parser := DurationScheduleParser{}
	after := time.Unix(100, 0)

	next, ok := parser.NextRunAfter("15m", after)
	assert.True(t, ok)
	assert.Equal(t, after.Add(15*time.Minute), next)

	next, ok = parser.NextRunAfter("every 1h", after)
	assert.True(t, ok)
	assert.Equal(t, after.Add(time.Hour), next)

	next, ok = parser.NextRunAfter("every friday 15:00", time.Date(2026, 8, 18, 12, 30, 0, 0, time.UTC))
	assert.True(t, ok)
	assert.Equal(t, time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC), next)

	next, ok = parser.NextRunAfter("every friday 15:00", time.Date(2026, 8, 21, 15, 30, 0, 0, time.UTC))
	assert.True(t, ok)
	assert.Equal(t, time.Date(2026, 8, 28, 15, 0, 0, 0, time.UTC), next)

	_, ok = parser.NextRunAfter("every day 09:00", after)
	assert.False(t, ok)

	_, ok = parser.NextRunAfter("0s", after)
	assert.False(t, ok)
}
