// A standalone module mimicking a real user city repo: it requires the PUBLISHED
// SDK and has no replace and no indirect deps. robocity-sim overrides the SDK with
// its local engine-backed copy via a temporary go.work, so this runs fully offline
// even though github.com/lyabah/simcode-sdk-go v0.0.1 is never downloaded.
module example.com/robocity-starter

go 1.23

require github.com/lyabah/simcode-sdk-go v0.0.1
