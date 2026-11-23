// What a kept trace records: screenshots and actions, no DOM snapshots. A
// DOM snapshot holds the value of every input, password fields included, so
// a typed password would sit in the trace; fillSecret keeps it out of the
// actions (trace-secrets.spec checks both together).
export const traceRecording = { screenshots: true, snapshots: false } as const
