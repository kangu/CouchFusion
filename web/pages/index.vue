<template>
  <section class="welcome-sheet">
    <header class="welcome-sheet__header">
      <p class="eyebrow">Workspace control plane</p>
      <h1>Your local stack,<br><em>under glass.</em></h1>
      <p class="welcome-sheet__lede">Register an existing CouchFusion workspace or initialize a new one. Nothing is discovered or modified until you choose it explicitly.</p>
    </header>

    <div class="welcome-sheet__rule"><span>Ready for connection</span></div>

    <div class="welcome-sheet__actions">
      <button class="action-button action-button--primary" type="button">
        <span>Register workspace</span><b aria-hidden="true">↗</b>
      </button>
      <button class="action-button" type="button">
        <span>Initialize new</span><b aria-hidden="true">＋</b>
      </button>
    </div>

    <dl class="system-strip">
      <div><dt>Server</dt><dd>Loopback only</dd></div>
      <div><dt>Registration</dt><dd>Explicit</dd></div>
      <div><dt>Runtime</dt><dd>Awaiting scan</dd></div>
    </dl>
  </section>
</template>
