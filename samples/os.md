---
title: Operating System
description: Operating System
aliases: []
tags:
  - cs
  - os
created: 2026-06-13
---
# Operating System

An operating system (OS) provides abstractions over hardware and manages the
resources that applications use.

## The OS as an Abstraction Layer

```text
Applications
    v
System calls (abstraction)
    v
Operating system
 +----------------+
 | Virtualization |
 | Concurrency    |
 | Persistence    |
 | Protection     |
 | Scheduling     |
 +----------------+
    v
Hardware
CPU   RAM   Disk   Devices
```

The OS hides hardware details behind reusable abstractions such as processes,
virtual memory, files, and system calls.

## Virtualization

Virtualization gives each program the illusion that it has its own resources.

### CPU virtualization

Even with one CPU core, multiple programs can appear to run at the same time:

```text
Program A runs -> timer interrupt -> OS pauses A
                  v
Program B runs -> timer interrupt -> OS pauses B
                  v
                ...
```

The OS creates this illusion by rapidly switching the CPU between runnable
tasks.

### Memory virtualization

Each process uses a virtual address space that the OS and hardware map to
physical memory. For example, different processes can use the same virtual
address without referring to the same physical address.

```text
Process A                         Process B
Virtual address space             Virtual address space
+---------------+                 +---------------+
| 0x0000        |---+         +---| 0x0000        |
| ...           |   |         |   | ...           |
| 0x1000        |---+         +---| 0x1000        |
+---------------+   |         |   +---------------+
                    v         v
                  +-----------------+
                  | Page tables     |
                  | OS + hardware   |
                  +-----------------+
                    |         |
                    v         v
Physical memory   +-----------------+
                  | Frame 0         |
                  | Frame 1         |
                  | Frame 2         |
                  | ...             |
                  +-----------------+
```

If physical memory is insufficient, the OS can use paging or swapping to move
less-used memory pages to storage temporarily.

## Process Memory Layout

The memory of one process is shared by all of its threads, but each thread has its own private stack:

```text
Process A: one shared address space
+--------------------------------------+
| Shared by Thread A and Thread B      |
|                                      |
| Code                                 |
| Global / static variables            |
| Heap                                 |
+--------------------------------------+
| Thread A: private stack              |
+--------------------------------------+
| Thread B: private stack              |
+--------------------------------------+
```

Each thread uses its stack for local variables, function calls, and return
addresses. Threads in the same process share the code, heap, and global or
static data. Shared heap and global data may require synchronization when
multiple threads access it concurrently.

> **Note:** A thread can access another thread’s stack memory if it has a valid
> pointer to that memory.

Separate processes have separate address spaces, so their heaps, global data,
and stacks are isolated from one another unless they explicitly use an
inter-process sharing mechanism.

## Concurrency

```text
Same process / shared address space

Goroutine 1       Goroutine 2       Goroutine 3
(private stack)   (private stack)   (private stack)
      |                 |                 |
      +---------+-------+-------+---------+
                v               v
          Shared heap      Global variables
                |               |
                +-------+-------+
                        v
              Mutex / channel / atomic
                 synchronization
```

The OS enables multiple tasks to make progress while coordinating access to
shared resources. Common synchronization primitives and concurrency concerns
include:

- Read-write locks
- Semaphores
- Mutexes
- Barriers
- Atomic operations, which cannot be interrupted halfway through
- Condition variables
- Deadlocks
- Race conditions
- Starvation

## Persistence

Common persistent storage devices include:

- SSDs
- Hard drives
- Flash storage

The OS provides a file-system abstraction. Instead of telling an SSD to write
bytes to physical flash blocks `83192–83195`, an application can use an
interface such as `open("abc.txt")`.

The file system must store more than file contents. File metadata can include:

- File size
- Owner
- Permissions
- Timestamps
- Locations of data blocks
- File type

The abstraction looks like this:

```text
Filename
   v
Directory entry
   v
Inode / metadata
   v
Disk blocks
   v
Actual data
```

Persistent storage introduces several problems:

- Latency
- Device failures
- Power loss
- Partial writes
- Limited write endurance
- Block allocation
- Fragmentation

## Resource Management

The OS manages resources such as:

```text
CPU cores: 8
RAM:       16 GB
SSD:       512 GB
```

- For CPU time, the **scheduler** decides which runnable process executes.
- For memory, the OS decides which physical RAM pages belong to each process.
- For storage, the file system decides where files are stored and which blocks
  are free.
- For devices, the OS manages access through device drivers.

## Scheduling

The scheduler decides how long a runnable task gets to execute before it may be
preempted. The duration is influenced by the scheduling policy, task priority,
system load, and whether the task blocks.

### Context Switching

```text
0 ms      Task A runs
5 ms      Switch
5-10 ms   Task B runs
10 ms     Switch
10-15 ms  Task C runs
```

- **Time slice / scheduling interval:** Often measured in milliseconds.
- **Context-switch cost:** Often much smaller, roughly on the microsecond scale,
  but it can become more expensive indirectly because caches, TLBs, and CPU
  pipelines may need to warm up again.

A task typically follows this lifecycle:

```text
Task is runnable
      v
Scheduler chooses it
      v
Task executes
      v
One of these happens:
  - Task finishes
  - Task blocks
  - Task voluntarily yields
  - Scheduler preempts it
```

## System Calls

System calls are the interface that an application can use to requests services
from the OS kernel.

```text
Application
    v
System call
    v
Operating-system kernel
    v
Hardware / resource
```

Not every OS primitive is exposed directly through an application programming
interface. Some functionality is provided indirectly by libraries or language
packages, which may call the OS on the application's behalf.
