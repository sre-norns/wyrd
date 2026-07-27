# SRE-Norns: Wyrd/Grace
A utilities package for graceful handling of OS signals.

For any service / process that needs to play nice with Kubernetes and handle termination signals gracefully using Go-lang context.
```go

func main() {
    // Setup proper K8s signal handlers for graceful termination
    mainContext := grace.NewSignalHandlingContext()
    ....
    // Use mainContext

    // mainContext - will be canceled by SIGTERM or SIGINT sent by a runtime

    grace.FatalOnError(service.Run(mainContext))
}

```

## Workgroup
A group of goroutines working on a stream of work items with a limited concurrency:
no more than a fixed number of work items are executed at any given time.

Two error handling policies are available:

`grace.NewWorkgroup` - fail-fast: the first work item to return an error cancels the group context.
Work items in-flight are expected to observe that cancellation. Items already accepted, including queued
items, are still invoked with the canceled context; later submissions are rejected:
```go

    workgroup := grace.NewWorkgroup(mainContext, runtime.NumCPU())
    for _, job := range jobs {
        // Go blocks while all workers are busy, and returns an error once the group has been canceled
        if err := workgroup.Go(func(ctx context.Context) error {
            return job.Run(ctx)
        }); err != nil {
            break
        }
    }

    // Wait returns the first non-cancellation work error, if any
    grace.FatalOnError(workgroup.Wait())

```

`grace.NewCollectingWorkgroup` - collect-all: every accepted work item is executed regardless of errors
reported by others, and `Wait` returns a multi-error that supports `errors.Is` for every cause:
```go

    workgroup := grace.NewCollectingWorkgroup(mainContext, runtime.NumCPU())
    for _, job := range jobs {
        _ = workgroup.Go(job.Run)
    }

    if err := workgroup.Wait(); err != nil {
        log.Printf("%d job(s) failed: %v", len(jobs), err)
    }

```

Cancellation of the parent context cancels the group under either policy, in which case `Wait` reports
the cause of that cancellation. If accepted work later reports a real failure, that failure is preserved
alongside the cancellation.

`Go` is safe to race with `Wait`: it either accepts an item that `Wait` waits for, or rejects it without
invoking it. Concurrent and repeated `Wait` calls return the same result. `Go(nil)` returns
`grace.ErrNilWorkItem`; calls after successful closure match `grace.ErrWorkgroupClosed`. A nil parent
context is treated as `context.Background`, and a non-positive worker count selects one worker.

`Wait` must be called to release resources held by the group context.
