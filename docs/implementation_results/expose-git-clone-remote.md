# Expose Git Clone Remote

## Initial Prompt
I have this error log: Enter app name: testing
Available modules: analytics, auth, content
Select modules (comma separated, leave empty for defaults): auth
Root path: /home/parallels/Sites
Enter HTTPS username: radu
Enter HTTPS password/token: 
Cloning into '/home/parallels/Sites/apps/testing'...
fatal: Remote branch stable not found in upstream origin
[ERROR] 2025/10/28 19:46:26 0.1.0 | create_app failed: git clone failed: exit status 128
 It seems the git clone failed. The git repository for sure is there, output the remote in the console for debugging before clone. Username and password put in are correct credentials for a forjego instance with basic authentication.

## Implementation Summary
Implementation Summary: Added a pre-clone log line reporting the repository URL, branch, and target directory before invoking git, aiding remote debugging without exposing injected credentials.

## Documentation Overview
- Extended the git utility so every clone announces its source repository, target branch, and destination path before any credential prompts or git subprocess execution.
- Debug output uses the configured repository URL, avoiding exposure of injected HTTPS credentials while still clarifying what remote the CLI is about to access.

## Implementation Examples
- `cli-init/internal/gitutil/git.go:23` prints the repository URL, branch, and target directory ahead of the git clone invocation.
