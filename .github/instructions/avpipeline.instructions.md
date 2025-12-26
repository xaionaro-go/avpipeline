# avpipeline-specific guidelines

## Goal of the project

The goal is to provide versitile and portable package for easy-to-build audio/video pipelines. It is essentially a high-level wrapper over libav that makes libav easy.

## How it works

There are 3 main abstractions:
- a "kernel" is what actually does packet or frame processing
- a "processor" is a wrapper over a kernel that manages inputs/outputs and does data flow control
- a "node" is a wrapper over a processor that manages wiring between processors

Multiple "nodes" together form a "pipeline".

The connections between nodes may be filtered by "filters".

A "kernel" may process incoming packets/frames (and as result may send these or other packets/frames to the output) or generate new packets/frames to the output on its own.

## Non-core features

There also various helpers are available to make pipeline building easier:
- "router" is a handler that may be used to publish or/and consume named streams.
- "monitor" is a debugging feature that allows to snoop on packets/frames.

## Rules

- a SEGFAULT is never fault of libav, it is always fault of our code and YOU MUST FIX IT.
