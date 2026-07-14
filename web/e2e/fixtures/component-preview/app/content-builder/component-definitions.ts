export default [
  {
    id: 'hero',
    label: 'Hero',
    previewComponentId: 'Hero',
    previewProps: { title: 'Ignored preview override' },
    props: [
      {
        key: 'title',
        label: 'Title',
        type: 'text',
        default: 'Declared preview default',
      },
    ],
  },
]
