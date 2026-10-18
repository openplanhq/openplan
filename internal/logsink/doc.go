// Package logsink carries a command's output from where it is written to where
// it is read: the spool file on the executor, and its live publication through
// a Valkey stream, one per run log, from the executor's flusher to the api's
// tailers until the log is uploaded. The stream's format (keys and entries)
// lives here too, since the executor writes it and the api both ends and reads
// it.
package logsink
