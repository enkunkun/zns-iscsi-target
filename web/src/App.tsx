import './App.css'
import { ZoneMap } from './components/ZoneMap'
import { DeviceInfo } from './components/DeviceInfo'
import { ISCSIPanel } from './components/ISCSIPanel'
import { GCStats } from './components/GCStats'
import { BufferGauge } from './components/BufferGauge'

function App() {
  return (
    <div className="app">
      <header className="app-header">
        <h1 className="app-title">ZNS iSCSI Target Dashboard</h1>
        <div className="app-header-meta">
          <span className="live-indicator">
            <span className="live-dot" />
            Live
          </span>
        </div>
      </header>

      <main className="dashboard">
        <div className="dashboard-row dashboard-row--top">
          <DeviceInfo />
          <ISCSIPanel />
        </div>

        <div className="dashboard-row dashboard-row--middle">
          <ZoneMap />
        </div>

        <div className="dashboard-row dashboard-row--bottom">
          <GCStats />
          <BufferGauge />
        </div>
      </main>
    </div>
  )
}

export default App
