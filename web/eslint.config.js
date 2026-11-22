import pluginVue from 'eslint-plugin-vue'
import { defineConfigWithVueTs, vueTsConfigs } from '@vue/eslint-config-typescript'
import skipFormatting from '@vue/eslint-config-prettier/skip-formatting'

export default defineConfigWithVueTs(
  { ignores: ['dist/**', 'node_modules/**', 'src/api/schema.d.ts', 'playwright-report/**', 'test-results/**'] },
  pluginVue.configs['flat/recommended'],
  vueTsConfigs.recommended,
  {
    rules: {
      // Components are imported where they are used; only Element Plus, the
      // router and the icons of src/app/icons.ts are registered globally.
      'vue/no-undef-components': [
        'error',
        {
          ignorePatterns: [
            '^el-',
            '^Router(Link|View)$',
            '^(ArrowDown|Download|Monitor|OfficeBuilding|Odometer|Plus|Refresh)$',
          ],
        },
      ],
    },
  },
  skipFormatting,
)
