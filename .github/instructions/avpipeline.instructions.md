# avpipeline-specific guidelines

## Goal

Versatile, portable Go library for building audio/video pipelines. High-level wrapper over libav (FFmpeg) that makes it easy to use.

## Architecture: Three-Layer Abstraction

```
Pipeline = [Node] ──filter──> [Node] ──filter──> [Node]
              │                   │                  │
          Processor           Processor          Processor
              │                   │                  │
           Kernel              Kernel             Kernel
```

- **Kernel**: Does actual packet/frame processing. May transform input→output or generate output independently.
- **Processor**: Wraps kernel; manages inputs/outputs, data flow control.
- **Node**: Wraps processor; manages wiring between processors.
- **Pipeline**: Multiple connected nodes. Connections between nodes can be filtered by **filters**.

### Helpers

- **Monitor**: Debug tool to snoop on packets/frames in transit.

## Rules

- A SEGFAULT is **never** libav's fault — it is always our code. YOU MUST FIX IT.
