## skills-switch source add

Add a vendor repository as a git submodule

```
skills-switch source add <git-url> [flags]
```

### Options

```
      --branch string                tracked branch (default "main")
      --client string                restrict the entire source to one registered client
      --discovery-priority strings   source discovery strategy priority (repeatable)
  -h, --help                         help for add
      --json                         emit JSON
      --name string                  source name
      --skill-path strings           authoritative Skill directory path (repeatable)
      --sparse strings               additional sparse-checkout path (repeatable)
```

### Options inherited from parent commands

```
      --lang string        interface language: auto, en, or zh (default "en")
      --project string     project directory (default current directory)
      --resources string   agent resources root (default ~/.agents/resources)
```

### SEE ALSO

* [skills-switch source](skills-switch_source.md)	 - Manage catalog source repositories

