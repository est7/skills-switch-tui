## skills-switch mcp add

Register a new MCP server in the catalog

```
skills-switch mcp add <name> [flags]
```

### Options

```
      --arg strings        stdio command argument (repeatable)
      --command string     stdio command executable
      --cwd string         stdio working directory
      --env strings        stdio environment KEY=VALUE (repeatable)
      --header strings     http header KEY=VALUE (repeatable)
  -h, --help               help for add
      --json               emit JSON
      --transport string   stdio or http (inferred when omitted)
      --url string         http(s) endpoint URL
```

### Options inherited from parent commands

```
      --lang string        interface language: auto, en, or zh (default "en")
      --project string     project directory (default current directory)
      --resources string   agent resources root (default ~/.agents/resources)
```

### SEE ALSO

* [skills-switch mcp](skills-switch_mcp.md)	 - Manage project-level MCP servers

