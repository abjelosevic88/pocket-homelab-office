import { Modal } from './ui'

const GROUPS: { title: string; keys: [string[], string][] }[] = [
  { title: 'Go to', keys: [[['h'], 'Home'], [['w'], 'Drafts'], [['1'], 'First place (1 – 9)'], [[','], 'Settings'], [['/'], 'Find a file & commands'], [['⌘', 'K'], 'Find a file & commands']] },
  { title: 'New', keys: [[['d'], 'Document'], [['s'], 'Spreadsheet'], [['p'], 'Presentation'], [['o'], 'Open a file from this device']] },
  { title: 'Window', keys: [[['['], 'Show or hide the sidebar'], [['esc'], 'Close a dialog or menu']] },
  { title: 'Help', keys: [[['?'], 'This sheet']] },
]

export function ShortcutsSheet({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Keyboard shortcuts" onClose={onClose} size="lg">
      <div className="shortcuts">
        {GROUPS.map(g => (
          <section key={g.title}>
            <h4 className="sc-h">{g.title}</h4>
            {g.keys.map(([keys, label]) => (
              <div key={label + keys.join('')} className="sc-row"><span>{label}</span><span className="sc-keys">{keys.map(k => <kbd key={k}>{k}</kbd>)}</span></div>
            ))}
          </section>
        ))}
      </div>
      <p className="muted small mt">New files land in My files and open right away. Inside the editor the shortcuts are the editor's own.</p>
    </Modal>
  )
}
