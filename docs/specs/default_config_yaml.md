The file at cli-init/internal/config/default_config.yaml should serve as the couchfusion default
config file in case the ~/.couchfusion/config.yaml file is not available.

- [x] Embed `internal/config/default_config.yaml` at build time for fallback usage. *(2025-10-28)*
- [x] Load the embedded defaults when no config path is provided and the user config is absent. *(2025-10-28)*
- [x] Emit a warning when the embedded defaults are used during startup. *(2025-10-28)*
