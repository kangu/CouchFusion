This is the initial draft of the PRD document for a CLI tool built with GO that handles initialization
of a new project based on the app + layers structure.

Functionality of the CLI:

When starting for the first time, identify if it's inside a "configured" state (done with "init") - so that there
are /app and /layers folders available.

Check if "bun" is installed and available in the system path. Show a warning message if not.
Check if couchdb is running on localhost:5984. Show a warning message if not.

0. config file
- holds the git repositories for "create_app", "create_layer", "init", with any optional authentication
  parameters needed for "git clone"

1. "init"

Creates the folder structure with "apps" and "layers" folders. The code from "layers" is cloned from a git
repository specified in a config file.

2. "create_app"
- Ask for app name
- Ask for which modules to include
  - Choose none/one/multiple none from a predefined list (matching the current layers setup)
- Creates the folder with the correct name inside /apps and does a git clone for the starter
  app as specified in the config file.

3. "create_layer"
- Ask for layer name
- Creates the folder with the correct name inside /layers and does a git clone for the starter
  layer as specified in the config file.
