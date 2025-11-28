# Default Config HTTPS

## Initial Prompt
Go for option 2 and make the changes to internal/config/default_config.yaml

## Implementation Summary
Implementation Summary: Switched the embedded default config to use HTTPS GitHub URLs (no auth prompt) so public repo clones work out of the box on fresh machines without SSH keys.

## Documentation Overview
- Updated the embedded fallback configuration to use HTTPS for init, app, and layer starter repositories, preventing SSH public key errors on new macOS installs.
- Disabled auth prompts for these public HTTPS repos to keep the installer non-interactive by default.
- Maintains the same branches and repository targets; only the transport and prompt behaviour changed.

## Implementation Examples
- `cli-init/internal/config/default_config.yaml` now points to `https://github.com/kangu/CouchFusion-BaseLayers.git` with `protocol: https` and `authPrompt: false` (similarly for new and create_layer).
