// The first date formatting in the app.
//
// Absolute rather than relative: "2 days ago" needs a clock, which makes every
// test that renders one depend on when it runs. The locale is pinned so the
// rendered string does not change with the runner's or the viewer's locale
// either — a fixed format is the point, not localisation.
const FORMATTER = new Intl.DateTimeFormat("en-GB", {
  day: "numeric",
  month: "short",
  year: "numeric"
});

export function formatTimestamp(isoTimestamp: string): string {
  const parsed = new Date(isoTimestamp);
  // An unparseable value yields NaN, and formatting that throws a RangeError.
  // Showing the raw string beats taking the screen down over a bad timestamp.
  if (Number.isNaN(parsed.getTime())) {
    return isoTimestamp;
  }
  return FORMATTER.format(parsed);
}

const DATE_TIME_FORMATTER = new Intl.DateTimeFormat("en-GB", {
  day: "numeric",
  month: "short",
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23"
});

// Day, month and time without the year, for events that are usually recent,
// such as runs: "21 Sep, 05:21". In the viewer's time zone; render it inside a
// <time dateTime> so the exact instant is still there to read.
export function formatDateTime(isoTimestamp: string): string {
  const parsed = new Date(isoTimestamp);
  if (Number.isNaN(parsed.getTime())) {
    return isoTimestamp;
  }
  return DATE_TIME_FORMATTER.format(parsed);
}

const TIME_FORMATTER = new Intl.DateTimeFormat("en-GB", {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23"
});

// The time alone, to the second, for steps under a header that already gives
// the day: "15:02:11". In the viewer's time zone, like formatDateTime.
export function formatTimeOfDay(isoTimestamp: string): string {
  const parsed = new Date(isoTimestamp);
  if (Number.isNaN(parsed.getTime())) {
    return isoTimestamp;
  }
  return TIME_FORMATTER.format(parsed);
}

// How long from one instant to the next, as CI systems print a step's time:
// "0s", "12s", "4m 5s", "1h 2m". Empty when either is unreadable or the end
// comes first, which says nothing rather than something wrong.
export function formatDuration(startIso: string, endIso: string): string {
  const milliseconds = new Date(endIso).getTime() - new Date(startIso).getTime();
  if (Number.isNaN(milliseconds) || milliseconds < 0) {
    return "";
  }
  const seconds = Math.floor(milliseconds / 1000);
  if (seconds < 60) {
    return `${seconds}s`;
  }
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) {
    return `${minutes}m ${seconds % 60}s`;
  }
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}
