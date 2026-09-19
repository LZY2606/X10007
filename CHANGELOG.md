# Changelog

## Unreleased

### Added

- `{{ yield super() }}`: inside a `{{block}}` definition that overrides another
  definition of the same name (via `extends` or `import`), `super` is a new
  keyword that invokes the overridden ("previous") definition. Parameters work
  like a regular `yield` (`{{ yield super(md=6) }}`); parameters not passed
  explicitly are inherited from the arguments bound in the enclosing block
  call, and otherwise fall back to the overridden definition's own defaults.
  The content form (`{{ yield super() content }}...{{ end }}`) is supported as
  well. Super chains span multiple levels of `extends`/`import`. Using
  `{{ yield super() }}` when there is no overridden definition is a runtime
  error. Which definition a `{{ yield super() }}` refers to is fixed at parse
  time: when the template is parsed, every overriding block definition is
  linked to the definition it overrides (in the merge order: extended
  template, then imports, then the template's own blocks), and that link never
  changes for the lifetime of the parsed template.

- `Set.Reload(templatePath)`: invalidates the cached parse result of a
  template and of every cached template that transitively depends on it via
  `extends`/`import`, so the next `GetTemplate()` re-fetches and re-parses
  them. The `Cache` interface is unchanged (it has no deletion method, so
  custom `Cache` implementations keep compiling and working). Instead of
  removing entries from the cache, the Set keeps a monotonically increasing
  generation counter plus a map from canonical template path to the
  generation at which it was invalidated; each parsed template records the
  generation at which it was parsed together with its transitive dependency
  closure. A cache entry is treated as stale — and the lookup falls through
  to the Loader — when any of its dependencies was invalidated after the
  template was parsed. Templates that do not depend on the reloaded path are
  served from the cache as before and never hit the Loader again.
