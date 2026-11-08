export default defineAppConfig({
  ui: {
    colors: {
      primary: 'emerald',
      neutral: 'zinc'
    },
    card: {
      slots: {
        root: 'rounded-xl border border-default bg-elevated/70 shadow-xs transition-all duration-150',
        body: 'p-4 sm:p-5'
      }
    }
  }
})
