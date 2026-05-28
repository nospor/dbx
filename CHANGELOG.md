
## [1.0.1] - 2026-05-28

### Features

- *(editor)* Support multiple query tabs per connection/database ([f1c9622](https://github.com/nospor/dbx/commit/f1c9622901b23e5d581a268db8be25d3e38c2ea9))

            - Add capability to open multiple query tabs for the same
            connection/database.
            - Assign unique sequence IDs (e.g. `#1`, `#2`) to tabs of the same
            connection, showing dynamic numbering labels (e.g. `(2)`, `(3)`).
            - Key the results pane cache and loading status by unique tab IDs so
            each tab retains independent results.
- *(ui/editor)* Remember cursor position and scroll state on tab switch ([5b09ba7](https://github.com/nospor/dbx/commit/5b09ba75f038c9307f5f31a0001afbc441eccd46))
- Support JSON and MongoDB query code blocks in AI pane ([5976e72](https://github.com/nospor/dbx/commit/5976e7270c70fcb12e537e6948d5d141edbd91e4))

### Bug Fixes

- *(query)* Strip comments before classifying query execution type ([41c109b](https://github.com/nospor/dbx/commit/41c109bc1f3414ea53e4d3c6fa51ac1da5acc79a))

            - Strips SQL comments before parsing the statement type to prevent
            queries
              with leading comments from being incorrectly routed to Exec.
            - Adds PRAGMA, VALUES, and EXEC to the list of prefixes routed to Query.
            - Fixes EXPLAIN query wrapping to ignore leading comments.
            - Adds test cases for the explain wrapper with comments.

### Performance

- *(editor)* Optimize query editor rendering and scroll layout ([5ae22b1](https://github.com/nospor/dbx/commit/5ae22b1a6fa2a03601d4e43c9bad505bbd2b6454))

            Avoid rendering-heavy syntax highlighting (Chroma) on non-visible lines
            and wrapping calculations, removing O(N) lag on cursor movement in
            large documents.
            - Add `linePlainForWrap` helper to wrap plain text instead of
            highlighted
              text when calculating heights and cursor offsets.
            - Skip highlighting logical lines in View() that are entirely scrolled
              out of the viewport.
            - Skip syntax highlighting on the active cursor line since it gets
              rendered as plain text with a reverse-video cursor anyway.

### Miscellaneous Tasks

- Update CHANGELOG.md for v1.0.0 [skip ci] ([8c88953](https://github.com/nospor/dbx/commit/8c889537c3af7ff996119595f47bdfe0e9b9ed90))

## [1.0.0] - 2026-05-25

### Bug Fixes

- *(db)* Fixing oracle db connection ([5b9af8d](https://github.com/nospor/dbx/commit/5b9af8d04833056b320f415288520a1be3406e17))
- *(oracle)* Resolve schema loading, quick-select, and DDL issues ([b538925](https://github.com/nospor/dbx/commit/b5389254684d90f0638ae8a5552d4abbada12fa1))

            - Prevent Oracle from treating the connection SID/Service Name as a
            schema, ensuring the explorer correctly fetches and displays actual
            database schemas.
            - Filter the schema list to only show schemas that contain tables or
            views, eliminating the clutter of empty default Oracle users.
            - Use direct uppercase string interpolation for metadata queries
            (tables, views, columns) to bypass brittle `go-ora` driver
            parameter-binding issues.
            - Change the `s` quick-select query syntax from `FETCH FIRST 100 ROWS
            ONLY` to `WHERE ROWNUM <= 100` to support older instances like Oracle
            11g.
            - Inject `DBMS_METADATA.GET_DDL` permission errors directly into the DDL
            popup text as SQL comments, preventing silent failures when users lack
            the `SELECT_CATALOG_ROLE`.

### Styling

- *(oracle)* Format oracle.go to resolve ci lint error ([773da3a](https://github.com/nospor/dbx/commit/773da3a65d68f0b07fd7ccaf2fcb0a5603a1e7b7))

### Miscellaneous Tasks

- Update CHANGELOG.md for v0.9.9 [skip ci] ([a49a3c0](https://github.com/nospor/dbx/commit/a49a3c05aef29d66661b9d12c3f536cccc4edbdd))
- Update CHANGELOG.md for v1.0.0 [skip ci] ([e57b2fa](https://github.com/nospor/dbx/commit/e57b2fad886403ba1361fcf92f65ad027b931d99))
- Resolve changelog rebase conflicts automatically ([d7dc6c1](https://github.com/nospor/dbx/commit/d7dc6c1cd1b0d78cea27e5a93059538a69f7650b))

## [0.9.9] - 2026-05-25

### Features

- *(ci)* Add CI/CD pipeline, GoReleaser, and git-cliff setup ([a7b999f](https://github.com/nospor/dbx/commit/a7b999f82a814cffdfc42e78dd75636d9e1b6700))

            - Integrate Cobra CLI for dynamic versioning
            - Add platform-independent process spawning helpers for Unix and Windows
            targets
            - Configure .goreleaser.yaml for multi-platform builds
            - Add cliff.toml for conventional commit changelog generation
            - Add GitHub Actions workflows for automated testing and release
            publishing

### Bug Fixes

- *(ci)* Format Go files, resolve Node 20 deprecation, and fix release config ([99a3d3d](https://github.com/nospor/dbx/commit/99a3d3de84064647f3138c126cec2aa2fecfb126))

            - Auto-format Go codebase files with gofmt
            - Force Node 24 for GitHub Actions via
            FORCE_JAVASCRIPT_ACTIONS_TO_NODE24 env var
            - Update GoReleaser archives format to use v2 plural formats key
            - Fix cliff.toml body template to render and indent commit descriptions
- *(ci)* Fix release asset collision and upgrade actions to v5/v6 ([f8c31a5](https://github.com/nospor/dbx/commit/f8c31a59ae970aa18107d098590edf7105fdb04d))
- *(ci)* Upgrade goreleaser-action to v7 ([570983c](https://github.com/nospor/dbx/commit/570983cc7fd5ab36a204a23b7faeef9b4308e4a2))
- *(ci)* Rebase changelog commit on main to avoid push rejection ([0d1ba77](https://github.com/nospor/dbx/commit/0d1ba77c968e7e98d2446b6603ec382ff9bf2eb0))

### Miscellaneous Tasks

- Update CHANGELOG.md for v0.9.9 [skip ci] ([399394d](https://github.com/nospor/dbx/commit/399394d39fdc2865aab0793423971ca293835556))
- Update CHANGELOG.md for v0.9.9 [skip ci] ([5a0886b](https://github.com/nospor/dbx/commit/5a0886bab04beecfe7e22af4f9ebf7ef2d2e34da))

## [0.9.8] - 2026-05-20

### Features

- Clear queries ([1cfe545](https://github.com/nospor/dbx/commit/1cfe545fe549f8bcdebd0a4094dde31db3355053))
- Copy all rows from results ([a6805ca](https://github.com/nospor/dbx/commit/a6805ca491d36e55b68e7588af9b3717fa51bad3))
- Copy rows as csv format ([dfa05d1](https://github.com/nospor/dbx/commit/dfa05d16014eed08f0cd2ac013849c3920d5f35a))

## [0.9.6] - 2026-05-14

### Features

- *(orientdb)* Http ([27ac66f](https://github.com/nospor/dbx/commit/27ac66fa4e457c945bad9a805461ff32c203b9f2))
- *(orientdb)* Http ([5ee2f1e](https://github.com/nospor/dbx/commit/5ee2f1e34d687ed3eb9b12ef866b6279d2e17209))
- *(orientdb)* Binary ([4d6ac7d](https://github.com/nospor/dbx/commit/4d6ac7d9345d378c7de144a904e83465b419ab05))
- Oracle db ([27c4f46](https://github.com/nospor/dbx/commit/27c4f4628f2e72337a44d5d637ffb7485ef50e85))
- Export all DDLs for given db ([fdbfc9e](https://github.com/nospor/dbx/commit/fdbfc9e3760b194eb099564bf790169eb72487fc))

## [0.9.5] - 2026-05-07

### Features

- Mongodb ([99ebac1](https://github.com/nospor/dbx/commit/99ebac1bd6359297e8127602e7e8b196ae397322))
- Mongo select first 100 ([b393493](https://github.com/nospor/dbx/commit/b393493ac8bfc525c946286f14d21000a90fa8f5))
- Mongodb d/u/i in results pane ([8800534](https://github.com/nospor/dbx/commit/8800534d91855106bea900c636fff0554918b68d))

### Bug Fixes

- Mongodb results pane ([5513835](https://github.com/nospor/dbx/commit/5513835c2a8a07317d3778e1b38162e3f0181325))

## [0.9.4] - 2026-04-15

### Features

- *(ai pane)* Basic markdown format in ai response ([468c08e](https://github.com/nospor/dbx/commit/468c08e9c23cb06b52c38bb9dd683e6ef11cf525))

## [0.9.3] - 2026-04-14

### Features

- Folder based query tabs ([eb4c051](https://github.com/nospor/dbx/commit/eb4c05109e8ea1c64ff8111f1a9c04ea9c31eea7))
- Query pane active by default ([02a4241](https://github.com/nospor/dbx/commit/02a42416007618da132946c11e432bebfd1404cb))

## [0.9.2] - 2026-04-14

### Features

- *(ai)* Now user can specify an ai folder to limit ai cli app context searching ([5b69d63](https://github.com/nospor/dbx/commit/5b69d638836e9f66e11a4898ce713a9ceedf55e2))

## [0.9.1] - 2026-04-13

### Features

- *(editor)* Explain ([bfb360a](https://github.com/nospor/dbx/commit/bfb360ad8124d81c2a5addf293357e32f2f59c6a))

## [0.9] - 2026-04-13

### Features

- First draft ai pane ([8eb7a72](https://github.com/nospor/dbx/commit/8eb7a721766e0f95c847f985ef6db8f76a22d2af))
- *(ai pane)* Clear command ([83516e7](https://github.com/nospor/dbx/commit/83516e75d4ab1c420d63d7f22b326d427508346b))
- Write ai config if not set ([18983bb](https://github.com/nospor/dbx/commit/18983bb4503d68a8c2ab8967618b38d344e10a2b))
- Remembering state of explorer and ai panes ([31c843e](https://github.com/nospor/dbx/commit/31c843eca6593444a0df4364959d5426e6348b0b))
- *(ai pane)* Alt + enter - new line ([6caf3f3](https://github.com/nospor/dbx/commit/6caf3f38ecf12fe9a7147fbfe49cce6336861984))
- *(ai pane)* Results can be send to ai now too ([821ad4a](https://github.com/nospor/dbx/commit/821ad4a5d1a05c41b104cbeaec372a42ca99db9a))

### Bug Fixes

- Pane view with visible cursor now ([0b34918](https://github.com/nospor/dbx/commit/0b349182af6a60d83cf1f5754bad4b0dce0fcfc7))
- Generating now DDLs for ai prompt ([b14bda4](https://github.com/nospor/dbx/commit/b14bda40bf123af8480ea4750a6101e9456f5f86))
- *(ai)* Enters copies proper query now ([780a02e](https://github.com/nospor/dbx/commit/780a02e282e27c106a504640070ec3f4e4a3cb40))
- *(ai pane)* Clearing quicker for clear command ([946b5ba](https://github.com/nospor/dbx/commit/946b5bae244030336cc6d0a66286b1acda8a2e25))
- *(ai pane)* Autocomplete popup position and scrolling ([1656577](https://github.com/nospor/dbx/commit/1656577963aed2b340e7a114a1e7225a0d6af7f5))
- *(ai pane)* Autocomplete popup fix ([5414b7f](https://github.com/nospor/dbx/commit/5414b7f7afd910779bc152e0590256dfd0a53707))
- *(ai pane)* Too long columns in autocomplete popup ([2232cf0](https://github.com/nospor/dbx/commit/2232cf0009c91238cc48abbf2c0a150090d99257))
- *(ai pane)* Sending prompt was frezing whole app ([edfea8f](https://github.com/nospor/dbx/commit/edfea8f32fd4a6b567c4129212421eaed79d745e))
- *(ai pane)* Respone of ai now goes to proper session when user switch to other one while waiting ([e975d4a](https://github.com/nospor/dbx/commit/e975d4ab85ac66145d1057ed61e4a73e1bd2b2fc))
- *(ai pane)* Proper inster of table/column in input ([27fad39](https://github.com/nospor/dbx/commit/27fad3929fd5de0dbb1b441a194a9275cbfddd9c))
- *(ai pane)* Sessions storage ([577ce42](https://github.com/nospor/dbx/commit/577ce4215ff2c2c71d86fb69b3fe63c98b949968))

## [0.8.1] - 2026-04-02

### Features

- Not adding top query when already exists ([ffc0013](https://github.com/nospor/dbx/commit/ffc00138d12d1c5072d30cb3cedddeebe3ff057e))

### Bug Fixes

- Undo not working when clearing all query pane tab ([5e56370](https://github.com/nospor/dbx/commit/5e56370601930f8aa01c235aad5bc1cd05229019))
- Fixing padding in table ([fe92080](https://github.com/nospor/dbx/commit/fe920802fd2b9e1014f08492a573b2e5c8362672))

## [0.8] - 2026-04-02

### Bug Fixes

- In full view jumping between panes ([f795f87](https://github.com/nospor/dbx/commit/f795f871fb637f0f0eeffb36cd7295bb0d264263))

## [0.7.9] - 2026-04-02

### Features

- Adding more commands for d/y (w, $, 0) ([cfc80c7](https://github.com/nospor/dbx/commit/cfc80c73112e21a1c85d8ff027ccb9b0fbe0714e))

### Bug Fixes

- Commands pallete ([a24f24f](https://github.com/nospor/dbx/commit/a24f24f99624e2dc4792a622dbd2c58eef661a19))

## [0.7.8] - 2026-04-02

### Features

- Better scrolling in explorer view ([13c3a26](https://github.com/nospor/dbx/commit/13c3a261709c05391b72b407cad405d3900e7c8d))
- Connection list now sorted ([80eb157](https://github.com/nospor/dbx/commit/80eb157b487e9c49149f97fe07613f035d147fa3))

### Bug Fixes

- Scrolling in ddl view ([6ef9925](https://github.com/nospor/dbx/commit/6ef9925b0c48f3fb15a468ec18780ae5570fbdba))
- Allow to use DEL in insert mode ([2b40ecc](https://github.com/nospor/dbx/commit/2b40eccb2e33e3e6b17c494c2168d56f1ad7484a))
- Fixing query deletion to delete also following line ([52f2e35](https://github.com/nospor/dbx/commit/52f2e35bf08843a6937bcd1742aa93a143d3238c))

## [0.7.7] - 2026-04-02

### Features

- Adding insert draft and fixing scrolling for wrapped queries ([8e45a84](https://github.com/nospor/dbx/commit/8e45a848e3578259eeb381c1d7befed4db746bf4))

## [0.7.6] - 2026-03-26

### Features

- Filtering tables ([a7f5845](https://github.com/nospor/dbx/commit/a7f5845b0c84f2821692fc4c985a88ba01581673))

## [0.7.5] - 2026-03-26

### Features

- Adding dd, dq, yy, yq in query pane normal mode ([416180c](https://github.com/nospor/dbx/commit/416180c2ae84cfca808a36f5001ee44dcb6a1ed9))
- D/y vim actions activates popup now too ([f5bc947](https://github.com/nospor/dbx/commit/f5bc9475f27774d12762a8b123865682d6bc7305))

## [0.7.4] - 2026-03-25

### Features

- Normal mode J/K to jump to next/prev queries ([0f0b38b](https://github.com/nospor/dbx/commit/0f0b38bb4eca32735580623dcdb650fe625904ca))
- Scrolling results by half page ([1ff6b6a](https://github.com/nospor/dbx/commit/1ff6b6a6c0566286f53fcccd8426bea094a5ac5f))

### Other

- Hints ([d9b4f0a](https://github.com/nospor/dbx/commit/d9b4f0ac71ee93b485c27e263ff326379e5dc05a))

## [0.7.3] - 2026-03-24

### Features

- Adding pgup/pgdown to results navigation ([b7c0e99](https://github.com/nospor/dbx/commit/b7c0e993e56726463ada9ef0305dafe18cac3926))

## [0.7.2] - 2026-03-23

### Features

- Filtering in history ([fd93bd8](https://github.com/nospor/dbx/commit/fd93bd86c2aa7ff4664a8632a3e33337aeeece38))
- Remembering last active tab/connection ([d74957d](https://github.com/nospor/dbx/commit/d74957d1f4244e238ad7f19b74aa00297db289af))

### Documentation

- Update readme ([e7c7e24](https://github.com/nospor/dbx/commit/e7c7e24fe8b00e1cf82355233f6b6bcd08793366))

## [0.7.1] - 2026-03-23

### Bug Fixes

- Timestamps ([81531c3](https://github.com/nospor/dbx/commit/81531c351a77293730ed24ed810c764273120e90))
- Suggestion popup ([e7e5daa](https://github.com/nospor/dbx/commit/e7e5daa6e0915ca778d904c948ff499cca441c6e))

## [0.7] - 2026-03-23

### Bug Fixes

- Batching delete/update queries ([f9d27b5](https://github.com/nospor/dbx/commit/f9d27b560f330bfa69e57a1324262fcbf533ac74))

## [0.6.9] - 2026-03-23

### Bug Fixes

- History popup ([1339380](https://github.com/nospor/dbx/commit/1339380380961373aa8de7b281526448f4b3c0b4))

## [0.6.8] - 2026-03-23

### Bug Fixes

- Fixes in popups ([14d7460](https://github.com/nospor/dbx/commit/14d74607e45c57af067a838afed5f11804d4a449))

## [0.6.7] - 2026-03-23

### Features

- Movement in cell view ([9fdc7ed](https://github.com/nospor/dbx/commit/9fdc7ed66b7ea278f0e7b91a586eda4c213dc6dc))
- Json format in view popup ([3a65615](https://github.com/nospor/dbx/commit/3a6561599ffc2bfef6904433ee46b0e9b8a207cf))

### Bug Fixes

- Loading tables when switching tabs ([0187c0d](https://github.com/nospor/dbx/commit/0187c0d255bc7b5321e98bbba4f83f03530a452f))
- Fixing popups ([7a4cb9f](https://github.com/nospor/dbx/commit/7a4cb9fbf229a19e872e64300b714a57fa6e474c))
- View cell jumping window ([f94bbae](https://github.com/nospor/dbx/commit/f94bbaec2f9db161f41568eb370541d21fb90e1b))

## [0.6.6] - 2026-03-20

### Other

- Just small labels changes ([92ee8c3](https://github.com/nospor/dbx/commit/92ee8c3869676260073ed9e0b0664d3da8d78027))

## [0.6.5] - 2026-03-20

### Bug Fixes

- Adding some space between queries area and tab label ([b241ecf](https://github.com/nospor/dbx/commit/b241ecffd1df32ef563682054bcb006d6cccf517))

## [0.6.4] - 2026-03-20

### Bug Fixes

- Jumping top borders ([ebe864a](https://github.com/nospor/dbx/commit/ebe864aa2147f9c70e311f753899a138e02ec2bb))

## [0.6.2] - 2026-03-20

### Bug Fixes

- Cleaning ([a0c8e62](https://github.com/nospor/dbx/commit/a0c8e624ee95fb239de8f3e404046733f438ccaf))

## [0.6.1] - 2026-03-20

### Features

- Query pane - tabs ([fa2a602](https://github.com/nospor/dbx/commit/fa2a602f5953e0bc9e0584f939dbc52a865b8b16))
- When switching between tabs, it collapsed previous connection ([848d170](https://github.com/nospor/dbx/commit/848d17008f5ce6a3a269d87087fc662ec564a7ef))

### Bug Fixes

- Each tab has now its own results ([04fc85e](https://github.com/nospor/dbx/commit/04fc85e3a13f62d2b0670988eead65aa645d219c))

### Other

- Readme ([899be0f](https://github.com/nospor/dbx/commit/899be0f8c46f6396aceba668aa37ef48646c337d))

## [0.6] - 2026-03-20

### Features

- Agents md ([947d31b](https://github.com/nospor/dbx/commit/947d31b432482cd675fc97e35ea133a8328c914f))
- Query content ([e571732](https://github.com/nospor/dbx/commit/e5717324bb735ce7f6f55631e0acd89f7fcdc939))
- V for view cell value ([74b25bc](https://github.com/nospor/dbx/commit/74b25bc66f85ad39c03b11be42dc3e06597e2140))
- H to collapse in explorer ([dbc9c73](https://github.com/nospor/dbx/commit/dbc9c73d43b115ae53385de1a60576f143f73e01))
- Columns list ([8ec0db7](https://github.com/nospor/dbx/commit/8ec0db72353871af6693e68d3f1c604811ccc62f))
- More informative ([620c0f7](https://github.com/nospor/dbx/commit/620c0f758b58e80dbce70227367a6fca13692ccc))
- More themes ([a812d49](https://github.com/nospor/dbx/commit/a812d49abe811602c55d87d211fa8f85ce597625))
- Seperate configs from data ([b69e09d](https://github.com/nospor/dbx/commit/b69e09debe8880f6f65eff451aa6fa0dfd7ae193))
- Autocomplete ([13d5ac1](https://github.com/nospor/dbx/commit/13d5ac16d6198abb9c0461fcabb9fa46205b6520))
- Loading all schemas on start ([d8c00e2](https://github.com/nospor/dbx/commit/d8c00e22655b5cc2a78c21368e620581cff75278))
- Undo/redo ([3424b7c](https://github.com/nospor/dbx/commit/3424b7cd6689200b78dad9a04904089fbb6c2743))
- Deleting rows ([3f8b48e](https://github.com/nospor/dbx/commit/3f8b48ef873a278763894cdf7524d821c5675f6f))
- Batch queries (delete/update) ([2792b93](https://github.com/nospor/dbx/commit/2792b9391100f2623e3e8d34dd0d4798d8828c18))
- Ddl for table ([6dc1e3e](https://github.com/nospor/dbx/commit/6dc1e3e32c502a88a2e815ee79b4b96eecaae23f))
- Update cell ([aa38aff](https://github.com/nospor/dbx/commit/aa38aff1cf300a78964cff1de6ae6ebbc884fc3e))
- Test connection ([590a12e](https://github.com/nospor/dbx/commit/590a12e570ae343633cdd2527952e176238da976))

### Bug Fixes

- Scroll indicator ([f93ac1f](https://github.com/nospor/dbx/commit/f93ac1fa725b203c65e32244dec961871ef26c05))
- Small fixes ([9593bf6](https://github.com/nospor/dbx/commit/9593bf6b7bd7449e310a5f805d9e6e5cab0b7b3b))
- History ([aa34ec5](https://github.com/nospor/dbx/commit/aa34ec5cf707c716e720d6131caf0ef68d7a4969))
- History layout ([d356eb5](https://github.com/nospor/dbx/commit/d356eb51098fd6e7d4770df025d5717935f69483))
- History ([244e772](https://github.com/nospor/dbx/commit/244e772c5ed60f4898cc1b0ab119ee498b1ed1d0))
- History ([ca7983d](https://github.com/nospor/dbx/commit/ca7983d9cc3b4aed06fd67bfd0f4babc10c7cb57))
- Query pane ([71660d6](https://github.com/nospor/dbx/commit/71660d6a4cf26443c7b5269aa281efa4c9a18e73))
- Commands popup ([c8a17a3](https://github.com/nospor/dbx/commit/c8a17a3e7f3db5462538dfd7ccd9efdb921a2692))
- Commands popup ([7be7d70](https://github.com/nospor/dbx/commit/7be7d70622f7df3d49c6515845c4d7ee23798eb7))
- Toggle explorer ([e2d5eba](https://github.com/nospor/dbx/commit/e2d5eba76e06fc7bf61e344f15412964d135e094))
- Expanding tables ([0035b5c](https://github.com/nospor/dbx/commit/0035b5c423e17eb3b30f222a3b1113f703305b7f))
- Labels in panes top borders ([a2d4fe4](https://github.com/nospor/dbx/commit/a2d4fe4a88dcc95500ce4819f4f0d44a629769dc))
- Dont jump to results pane after pressing s ([b58eb22](https://github.com/nospor/dbx/commit/b58eb22f783028153a476ba75ef6a6f80e6df33b))
- Active cell visible ([9f0a0e6](https://github.com/nospor/dbx/commit/9f0a0e68ba0a7c8bca576129dc9d5cd3caeb0f80))
- Jumping top labels ([5f896c7](https://github.com/nospor/dbx/commit/5f896c7d792fdd57a2b1f2c184f927fa3a1a92d6))
- Showing array of bytes instead of text ([7a56e6a](https://github.com/nospor/dbx/commit/7a56e6aa6523100f11a2a4d96a80aa5c0147ee7b))
- Fixing default theme a bit ([4986263](https://github.com/nospor/dbx/commit/498626334e3eb07943d06457639287e12a8414d0))
- Autocomplete case not sensitive ([3e68cc1](https://github.com/nospor/dbx/commit/3e68cc12ae3bb48b14e0389cd28a1f93d1efbaaa))
- Tab now triggers autocomplete too ([ccfc63b](https://github.com/nospor/dbx/commit/ccfc63bed1a123b24005e08e4b2e46aa34b90c68))
- S doesnt override current query view ([f46f2a1](https://github.com/nospor/dbx/commit/f46f2a10bb9949696c63afcd5faacddf4600c560))
- Help popup more beatifull now ([49a7e64](https://github.com/nospor/dbx/commit/49a7e64caf6df6f03d799fc2682d864db7363a89))
- Polishing a bit hints ([6fcdb40](https://github.com/nospor/dbx/commit/6fcdb40333644115a2b2524aab856a5eff5555e2))
- Update query with blank line ([010fb93](https://github.com/nospor/dbx/commit/010fb93a7479fd6933f7d256329924110da3030e))
- Jumping active element on commands popup ([f65416c](https://github.com/nospor/dbx/commit/f65416cbe963ad2a81f588a2e9da5e63bff1a667))

### Other

- Readme ([2124b05](https://github.com/nospor/dbx/commit/2124b050925d5076689eac35500759bb0a655430))
