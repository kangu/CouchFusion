Each layer that is available for install has a set of changes it needs to perform on the main
Nuxt configuration file at nuxt.config.ts. The changes are to be executed once the repository
has been cloned.

Each layer that is selected is supposed to end up in the top level "extends" array. At the beginning
of the process, discard everything that is present in the "extends" array (currenttly that's
extends: [
        '../../layers/auth',
        '../../layers/orders',
        '../../layers/database',
        '../../layers/content',
        '../../layers/analytics',
        '../../layers/imagekit'
]) and keep only the picked layers, like  '../../layers/auth' for example.

- [x] After cloning a new app, replace the `extends` array in `nuxt.config.ts` with only the selected layers using `../../layers/<module>` entries. *(2025-10-28)*
- [x] Remove any existing `extends` entries so unselected layers are discarded before persisting the new list. *(2025-10-28)*
