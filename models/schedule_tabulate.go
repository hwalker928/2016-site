package models

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/UniversityRadioYork/2016-site/utils"
)

//
// Week schedule algorithm
// TODO(CaptainHayashi): move?
//

// WeekScheduleCell represents one cell in the week schedule.
type WeekScheduleCell struct {
	RowSpan uint

	// Pointer to the timeslot in this cell, if any.
	// Will be nil if 'RowSpan' is 0.
	Item *ScheduleItem

	// Hour stores which hour (row) the cell is in
	Hour int

	// Minute stores the minute for this row
	Minute int
}

// WeekScheduleRow represents one row in the week schedule.
type WeekScheduleRow struct {
	// The hour of the row (0..23).
	Hour int
	// The minute of the show (0..59).
	Minute int
	// The cells inside this row.
	Cells []WeekScheduleCell
}

// addCell adds a cell with rowspan s and item i to the row r.
func (r *WeekScheduleRow) addCell(s uint, i *ScheduleItem) {
	r.Cells = append(r.Cells, WeekScheduleCell{RowSpan: s, Item: i})
}

// straddlesDay checks whether a show's start and finish cross over the boundary of a URY day.
func straddlesDay(s *ScheduleItem) bool {
	dayBoundary := utils.StartHour
	adjustedStart := s.Start.Add(time.Hour * time.Duration(-dayBoundary))
	adjustedEnd := s.Finish.Add(time.Hour * time.Duration(-dayBoundary))
	straddle := adjustedEnd.Day() != adjustedStart.Day() && s.Finish.Sub(s.Start) > time.Hour
	return straddle
}

// calcScheduleBoundaries gets the offsets of the earliest and latest visible schedule hours.
// It returns these as top and bot respectively.
func calcScheduleBoundaries(items []*ScheduleItem, scheduleStart time.Time) (top, bot utils.StartOffset, err error) {
	if len(items) == 0 {
		err = errors.New("calculateScheduleBoundaries: no schedule")
		return
	}

	// These are the boundaries for culling, and are expanded upwards when we find shows that start earlier or finish later than the last-set boundary.
	// Initially they are set to one past their worst case to make the updating logic easier.
	// Since we assert we have a schedule, these values _will_ change.
	// (Top must be before 00:00 or the populator gets screwed up)
	top = utils.StartOffset(23 - utils.StartHour)
	bot = utils.StartOffset(-1)

	for _, s := range items {
		// Any show that isn't a sustainer affects the culling boundaries.
		if s.IsSustainer() {
			continue
		}

		if straddlesDay(s) {
			if scheduleStart.After(s.Start) {
				//This is the first item on the schedule and straddles the week, so we only set the top of the schedule
				//top = utils.StartOffset(0)
				//Temporarily disabled as this slot doesn't show up on the schedule
				continue
			} else if s.Finish.After(scheduleStart.AddDate(0, 0, 7)) {
				//This is the last item on the schedule and straddles the week, so we only set the bottom of the schedule
				bot = utils.StartOffset(23)
				continue
			} else {
				// An item that straddles the day crosses over from the end of a day to the start of the day.
				// This means that we saturate the culling boundaries.
				// As an optimisation we don't need to consider any other show.
				return utils.StartOffset(0), utils.StartOffset(23), nil
			}
		}

		// Otherwise, if its start/finish as offsets from start time are outside the current boundaries, update them.
		var ctop utils.StartOffset
		if ctop, err = utils.HourToStartOffset(s.Start.Hour()); err != nil {
			return
		}
		if ctop < top {
			top = ctop
		}

		var cbot utils.StartOffset
		if cbot, err = utils.HourToStartOffset(s.Finish.Hour()); err != nil {
			return
		}
		// cbot is the offset of the hour in which the item finishes.
		// This is _one past_ the last row the item occupies if the item ends cleanly at :00:00.
		if s.Finish.Minute() == 0 && s.Finish.Second() == 0 && s.Finish.Nanosecond() == 0 {
			cbot--
		}

		if bot < cbot {
			bot = cbot
		}
	}

	return
}

// rowDecision is an internal type recording information about which rows to display in the week schedule.
// It records, for one hour, the minute rows (00, 30, etc) that are switched 'on' for that row.
type rowDecision map[int]struct{}

// visible checks if the hour represented by row decision r is to be shown on the schedule.
func (r rowDecision) visible() bool {
	// Each visible row has its on-the-hour row set.
	_, visible := r[0]
	return visible
}

// mark adds a mark for the given minute to row decision r.
func (r rowDecision) mark(minute int) {
	r[minute] = struct{}{}
}

// toRow converts row decision r to a slice of schedule rows for the given hour.
func (r rowDecision) toRows(hour int) []WeekScheduleRow {
	minutes := make([]int, len(r))
	j := 0
	for k := range r {
		minutes[j] = k
		j++
	}
	sort.Ints(minutes)

	rows := make([]WeekScheduleRow, len(minutes))
	for j, m := range minutes {
		rows[j] = WeekScheduleRow{Hour: hour, Minute: m, Cells: []WeekScheduleCell{}}
	}
	return rows
}

// initRowDecisions creates 24 rowDecisions, from schedule start to schedule end.
// Each is marked as visble or invisible depending on the offsets top and bot.
func initRowDecisions(top, bot utils.StartOffset) ([]rowDecision, error) {
	// Make sure the offsets are valid.
	if !top.Valid() || !bot.Valid() {
		return nil, fmt.Errorf("initRowDecisions: row boundaries %d to %d are invalid", int(top), int(bot))
	}

	rows := make([]rowDecision, 24)

	// Go through each hour, culling ones before the boundaries, and adding on-the-hour minute marks to the others.
	// Boundaries are inclusive, so cull only things outside of them.
	for i := utils.StartOffset(0); i < utils.StartOffset(24); i++ {
		h, err := i.ToHour()
		if err != nil {
			return nil, err
		}

		rows[h] = rowDecision{}
		if top <= i && i <= bot {
			// This has the effect of making the row visible.
			rows[h].mark(0)
		}
	}

	return rows, nil
}

// addItemsToRowDecisions populates the row decision list rows with minute marks from schedule items not starting on the hour.
func addItemsToRowDecisions(rows []rowDecision, items []*ScheduleItem) {
	for _, item := range items {
		h := item.Start.Hour()
		if rows[h].visible() {
			rows[h].mark(item.Start.Minute())
		}
	}
}

// rowDecisionsToRows generates rows based on the per-hourly row decisions in rdecs.
func rowDecisionsToRows(rdecs []rowDecision) ([]WeekScheduleRow, error) {
	rows := []WeekScheduleRow{}

	for i := utils.StartOffset(0); i < utils.StartOffset(24); i++ {
		h, err := i.ToHour()
		if err != nil {
			return nil, err
		}

		if rdecs[h].visible() {
			rows = append(rows, rdecs[h].toRows(h)...)
		}
	}

	return rows, nil
}

// initScheduleRows takes a schedule and determines which rows should be displayed.
func initScheduleRows(items []*ScheduleItem, startTime time.Time) ([]WeekScheduleRow, error) {
	top, bot, err := calcScheduleBoundaries(items, startTime)
	if err != nil {
		return nil, err
	}

	rdecs, err := initRowDecisions(top, bot)
	if err != nil {
		return nil, err
	}
	addItemsToRowDecisions(rdecs, items)

	return rowDecisionsToRows(rdecs)
}

// populateRows fills schedule rows with timeslots.
// It takes the list of schedule start times on the days the schedule spans,
// the slice of rows to populate, and the schedule items to add.
func populateRows(days []time.Time, rows []WeekScheduleRow, items []*ScheduleItem) {
	currentItem := 0

	for d, day := range days {
		// We use this to find out when we've gone over midnight
		lastHour := -1
		// And this to find out where the current show started
		thisShowIndex := -1

		// Now, go through all the rows for this day.
		// We have to be careful to make sure we tick over day if we go past midnight.
		for i := range rows {
			if rows[i].Hour < lastHour {
				day = day.AddDate(0, 0, 1)
			}
			lastHour = rows[i].Hour

			rowTime := time.Date(day.Year(), day.Month(), day.Day(), rows[i].Hour, rows[i].Minute, 0, 0, time.Local)

			// Seek forwards if the current show has finished.
			for !items[currentItem].Finish.After(rowTime) {
				currentItem++
				thisShowIndex = -1
			}

			// If this is not the first time we've seen this slot,
			// update the rowspan in the first instance's cell and
			// put in a placeholder.
			if thisShowIndex != -1 {
				rows[thisShowIndex].Cells[d].RowSpan++
				rows[i].addCell(0, nil)
			} else {
				thisShowIndex = i
				rows[i].addCell(1, items[currentItem])
			}
		}
	}
}

// WeekSchedule is the type of week schedules.
type WeekSchedule struct {
	// Dates enumerates the dates this week schedule covers.
	Dates []time.Time
	// Table is the actual week table.
	// If there is no schedule for the given week, this will be nil.
	Table []WeekScheduleCol
	// The week's schedule but in list form not table form
	// If there is no schedule for the given week, this will be nil.
	List []WeekScheduleList
}

// hasShows asks whether a schedule slice contains any non-sustainer shows.
// It assumes the slice has been filled with sustainer.
func hasShows(schedule []*ScheduleItem) bool {
	// This shouldn't happen, but if it does, this is the right thing to do.
	if len(schedule) == 0 {
		return false
	}

	// We know that, if a slice is filled but has no non-sustainer, then
	// the slice will contain only one sustainer item.  So, eliminate the
	// other cases.
	if 1 < len(schedule) || !schedule[0].IsSustainer() {
		return true
	}

	return false
}

// Flippin that table

// WeekScheduleCol represents one day in the week schedule.
type WeekScheduleCol struct {
	// The day of the show.
	Day time.Time
	// The cells inside this row.
	Cells []WeekScheduleCell
}

// addCell adds a cell with rowspan s and item i to the column c.
func (c *WeekScheduleCol) addCell(s uint, i *ScheduleItem, h int, m int) {
	c.Cells = append(c.Cells, WeekScheduleCell{RowSpan: s, Item: i, Hour: h, Minute: m})
}

type WeekScheduleList struct {
	Day     time.Time
	Current bool
	Shows   []ScheduleItem
}

// tableFilp flips the schedule table such that it becomes a list of days which have a list
// of shows on that day.
func tableFilp(rows []WeekScheduleRow, dates []time.Time) []WeekScheduleCol {
	days := make([]WeekScheduleCol, 7)
	for i := range days {
		days[i].Day = dates[i]
	}
	for _, row := range rows {
		for i, cell := range row.Cells {
			days[i].addCell(cell.RowSpan, cell.Item, row.Hour, row.Minute)
		}
	}
	return days
}

func buildList(schedule []*ScheduleItem, dates []time.Time) []WeekScheduleList {
	thisYear, thisMonth, thisDay := time.Now().Date()
	days := make([]WeekScheduleList, 7)
	thisWeek := false
	for i := range days {
		days[i].Day = dates[i]
		year, month, day := dates[i].Date()
		if year == thisYear && month == thisMonth && day == thisDay {
			days[i].Current = true
			thisWeek = true
		}
	}
	if !thisWeek {
		days[0].Current = true
	}
	for _, item := range schedule {
    dayIndex := (item.Start.Weekday() + 6) % 7
		if straddlesDay(item) {
      item.ShowWeekDay = true
      EnddayIndex := (item.Finish.Weekday() + 6) % 7
      for i := dayIndex; i<=EnddayIndex; i++ {
        days[i].Shows = append(days[i].Shows, *item);
      }
		} else {
			days[dayIndex].Shows = append(days[dayIndex].Shows, *item)
		}
	}
  	for day := range days {
		// Where does the come from, nobody knows; here's a fix to get rid of it though -ash (2024)
		// TODO: actually fix this
		// it comes from makescheduleslice see the s.fill line. needed otherwise the first show on monday will start at 6am on table view
		// or not, theres jukeboxes coming from elsewhere aswell
		if len(days[day].Shows) > 0 {
			if days[day].Shows[len(days[day].Shows)-1].IsSustainer(){
				days[day].Shows = days[day].Shows[:len(days[day].Shows) - 1]
			}
			if days[day].Shows[0].IsSustainer(){
				days[day].Shows = days[day].Shows[1:]
    		}
    	}
  	}
	return days
}

// tabulateWeekSchedule creates a schedule table from the given schedule slice.
func tabulateWeekSchedule(start, finish time.Time, schedule []*ScheduleItem) (*WeekSchedule, error) {
	days := []time.Time{}
	for d := start; d.Before(finish); d = d.AddDate(0, 0, 1) {
		days = append(days, d)
	}

	if !hasShows(schedule) {
		return &WeekSchedule{
			Dates: days,
			Table: nil,
			List:  nil,
		}, nil
	}

	rows, err := initScheduleRows(schedule, start)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	populateRows(days, rows, schedule)

	table := tableFilp(rows, days)
	list := buildList(schedule, days)

	sch := WeekSchedule{
		Dates: days,
		Table: table,
		List:  list,
	}

	return &sch, nil
}
