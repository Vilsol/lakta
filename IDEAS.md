This file is just a list of ideas (in no particular order) that may or may not be implemented, depending on whether they are useful or feasible.

General ideas:

* Web interface with graphs, metrics, live config editing, DI tree, module health, logs, errors, etc. Think of something like traefik dashboard but more.
* Log level context propagation over GRPC and HTTP to upstream services. (they would also have to be lakta services as afaik there is no standard for this)
* Test runtime harness.
* Splitting up packages into separate modules? (or some other way to reduce the amount of dependencies at root)
* Module dependency graph. Currently the modules are loaded in the order they are defined in the runtime. Maybe could use some sort of importance or ranking or something?
* Pub/Sub between modules? Would this be useful?

Module ideas:

* Caching module: utilizing valkey or in-memory with a super simple Get/Set/Delete interface.
* Cron module: provides a cron-like interface for scheduling tasks.
* Feature flag module: provides a simple interface for feature flagging. Would also be nice to have A/B testing possiblity. (would tie in nicely with web interface)
