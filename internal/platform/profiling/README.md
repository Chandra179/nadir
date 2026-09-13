# Profiling

This platform module owns the optional Go `pprof` listener. It is disabled in
the shipped configuration. When enabled, configuration validation permits only
loopback addresses, keeping profiling outside the public API listener.

Use `PROFILING_ENABLED=true PROFILING_ADDR=127.0.0.1:6063` for a local
diagnostic session. For a remote host, reach it through a protected path such
as an SSH tunnel (`ssh -L 6063:127.0.0.1:6063 host`) rather than binding pprof
to a public interface. Stop the API after collecting the profile.
