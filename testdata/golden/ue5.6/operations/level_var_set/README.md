# level_var_set

Generated operation fixture.

- command: `level var-set`
- expect: `error_equal`
- notes: Level var-set currently rejects NavigationSystemConfig because UE save also compacts related import/export/name state.
- output: `before.umap`, `after.umap`, `operation.json`
