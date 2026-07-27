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
Work items in-flight are expected to observe that cancellation, items still queued are dropped:
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

    // Wait returns the first error reported by a work item, if any
    grace.FatalOnError(workgroup.Wait())

```

`grace.NewCollectingWorkgroup` - collect-all: every work item submitted is executed regardless of errors
reported by others, and `Wait` returns all of the errors joined with `errors.Join`:
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
the cause of that cancellation. `Wait` must be called to release resources held by the group context.
