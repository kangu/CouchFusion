At the end of a successful `couchfusion new` workflow, instruct the user how to continue.

The workflow currently performs these setup steps before presenting the final instructions:

- Clone the starter app into `apps/${project_name}`.
- Reinitialize the cloned starter as a fresh git repository.
- Add selected local layer dependencies to `package.json`.
- Apply selected layer parameter handlers. The `auth` handler writes CouchDB credentials and cookie secret values into `.env`.
- Rewrite `nuxt.config.ts` so `extends` contains only the selected layers.
- Write `couchfusion.json` and `docs/module_setup.json`.

Steps:
1. Navigate to the project directory:
   ```bash
   cd apps/${project_name}
   ```
2. Install dependencies:
   ```bash
   bun install
   ```
3. Start the development server:
   ```bash
   bun run dev
   ```
