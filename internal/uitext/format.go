package uitext

import (
	"strconv"
	"strings"
	"time"
)

// Dates and numbers are worded like anything else. Go writes the names
// of months in English and a decimal with a point, whatever the reader
// reads; so the parts of a date are put into a message, which says what
// order they go in and what goes between them, and a number is written
// with the decimal mark the language uses.
//
// The names of the months and the patterns are in web/text with the
// rest of the wording, where a translator finds them, and not in a
// library that would know more languages than the interface is
// written in.

var monthsLong = [12]ID{
	"format.month.long.january", "format.month.long.february", "format.month.long.march",
	"format.month.long.april", "format.month.long.may", "format.month.long.june",
	"format.month.long.july", "format.month.long.august", "format.month.long.september",
	"format.month.long.october", "format.month.long.november", "format.month.long.december",
}

var monthsShort = [12]ID{
	"format.month.short.january", "format.month.short.february", "format.month.short.march",
	"format.month.short.april", "format.month.short.may", "format.month.short.june",
	"format.month.short.july", "format.month.short.august", "format.month.short.september",
	"format.month.short.october", "format.month.short.november", "format.month.short.december",
}

// Day is a date in full: "2 January 2006".
func (l Localizer) Day(t time.Time) string {
	return l.T("format.date.day", Args{
		"Day": t.Day(), "Month": l.T(monthsLong[t.Month()-1]), "Year": t.Year(),
	})
}

// DateTime is a date and a time, in brief: "2 Jan 2006, 15:04".
func (l Localizer) DateTime(t time.Time) string {
	return l.T("format.date.time", Args{
		"Day": t.Day(), "Month": l.T(monthsShort[t.Month()-1]), "Year": t.Year(),
		"Time": t.Format("15:04"),
	})
}

// ShortDay is a day of a year that goes without saying: "2 Jan".
func (l Localizer) ShortDay(t time.Time) string {
	return l.T("format.date.short", Args{
		"Day": t.Day(), "Month": l.T(monthsShort[t.Month()-1]),
	})
}

// Decimal is a number to so many places, with the language's decimal
// mark.
func (l Localizer) Decimal(value float64, places int) string {
	return l.mark(strconv.FormatFloat(value, 'f', places, 64))
}

// Signed is Decimal with its sign, plus as well as minus: a score that
// can fall either side of nought.
func (l Localizer) Signed(value float64, places int) string {
	written := strconv.FormatFloat(value, 'f', places, 64)
	if !strings.HasPrefix(written, "-") {
		written = "+" + written
	}
	return l.mark(written)
}

// Count is a whole number with its digits grouped in threes by the
// language's separator: "1,000,000" in English.
func (l Localizer) Count(n int64) string {
	digits := strconv.FormatInt(n, 10)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	if len(digits) <= 3 {
		return sign + digits
	}
	group := l.T("format.group")
	var b strings.Builder
	lead := len(digits) % 3
	if lead > 0 {
		b.WriteString(digits[:lead])
	}
	for i := lead; i < len(digits); i += 3 {
		if b.Len() > 0 {
			b.WriteString(group)
		}
		b.WriteString(digits[i : i+3])
	}
	return sign + b.String()
}

func (l Localizer) mark(written string) string {
	return strings.Replace(written, ".", l.T("format.decimal"), 1)
}

// Percent is a whole percentage: "42%".
func (l Localizer) Percent(value int) string {
	return l.T("format.percent", Args{"Value": value})
}

// Euros is an amount of money: "€1.50".
func (l Localizer) Euros(value float64) string {
	return l.T("format.money.euro", Args{"Value": l.Decimal(value, 2)})
}

// Size is the size of a file, or nothing for a size that is not known.
func (l Localizer) Size(bytes int64) string {
	switch {
	case bytes <= 0:
		return ""
	case bytes < 1<<20:
		return l.T("format.size.kilobytes", Args{"Value": bytes/1024 + 1})
	default:
		return l.T("format.size.megabytes", Args{"Value": l.Decimal(float64(bytes)/(1<<20), 1)})
	}
}

// Duration is a length of time in minutes and seconds, or nothing for
// none.
func (l Localizer) Duration(seconds int) string {
	switch {
	case seconds <= 0:
		return ""
	case seconds < 60:
		return l.T("format.duration.seconds", Args{"Seconds": seconds})
	default:
		// The seconds of a minute are written with two digits: "2m 05s".
		rest := strconv.Itoa(seconds % 60)
		if len(rest) < 2 {
			rest = "0" + rest
		}
		return l.T("format.duration.minutes", Args{"Minutes": seconds / 60, "Seconds": rest})
	}
}
