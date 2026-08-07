import React, { useState, useEffect } from 'react';
import './App.css';

function App() {
  const [activeTab, setActiveTab] = useState('database'); // 'database' or 'auth'
  
  // Database Explorer States
  const [scopes, setScopes] = useState([]);
  const [activeScope, setActiveScope] = useState('');
  const [activeCollection, setActiveCollection] = useState('');
  const [documents, setDocuments] = useState([]);
  const [showModal, setShowModal] = useState(false);
  const [modalDocID, setModalDocID] = useState('');
  const [modalDocBody, setModalDocBody] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  // Authentication Tab States
  const [users, setUsers] = useState([]);
  const [userSearch, setUserSearch] = useState('');
  
  // OIDC Credentials Form
  const [oidcProvider, setOidcProvider] = useState('google');
  const [oidcClientId, setOidcClientId] = useState('');
  const [oidcClientSecret, setOidcClientSecret] = useState('');
  
  // SAML SSO Config Form
  const [samlTenant, setSamlTenant] = useState('corporate');
  const [samlEntityId, setSamlEntityId] = useState('');
  const [samlSsoUrl, setSamlSsoUrl] = useState('');
  const [samlXmlMetadata, setSamlXmlMetadata] = useState('');

  // 1. Fetch DB Schema on mount
  useEffect(() => {
    fetchSchema();
  }, []);

  // 2. Fetch Documents when active collection changes
  useEffect(() => {
    if (activeScope && activeCollection && activeTab === 'database') {
      fetchDocuments(activeScope, activeCollection);
    }
  }, [activeScope, activeCollection, activeTab]);

  // 3. Fetch Users when switching to Auth tab
  useEffect(() => {
    if (activeTab === 'auth') {
      fetchUsers();
    }
  }, [activeTab]);

  const fetchSchema = async () => {
    try {
      const res = await fetch('/rest/v1/db/schema');
      const data = await res.json();
      if (data.scopes && data.scopes.length > 0) {
        setScopes(data.scopes);
        const firstScope = data.scopes[0];
        if (firstScope.collections && firstScope.collections.length > 0) {
          setActiveScope(firstScope.name);
          setActiveCollection(firstScope.collections[0]);
        }
      }
    } catch (err) {
      console.error('Failed to load schema:', err);
      setError('Connection to Couchbase Gateway failed.');
    }
  };

  const fetchDocuments = async (scope, collection) => {
    setLoading(true);
    try {
      const res = await fetch(`/rest/v1/db/${scope}/${collection}`);
      const data = await res.json();
      if (Array.isArray(data)) {
        setDocuments(data);
      } else {
        setDocuments([]);
      }
    } catch (err) {
      console.error('Failed to load documents:', err);
      setDocuments([]);
    } finally {
      setLoading(false);
    }
  };

  const fetchUsers = async () => {
    setLoading(true);
    try {
      const res = await fetch('/rest/v1/db/auth/users');
      const data = await res.json();
      if (Array.isArray(data)) {
        setUsers(data);
      } else {
        setUsers([]);
      }
    } catch (err) {
      console.error('Failed to load users:', err);
      setUsers([]);
    } finally {
      setLoading(false);
    }
  };

  const handleDelete = async (id) => {
    if (!window.confirm(`Delete document ${id}?`)) return;

    try {
      const res = await fetch(`/rest/v1/db/${activeScope}/${activeCollection}/${id}`, {
        method: 'DELETE',
      });
      if (res.ok) {
        fetchDocuments(activeScope, activeCollection);
      } else {
        alert('Failed to delete document.');
      }
    } catch (err) {
      console.error('Delete failed:', err);
    }
  };

  const openCreateModal = () => {
    setModalDocID('');
    setModalDocBody('{\n  "owner_id": "usr_123",\n  "status": "active"\n}');
    setShowModal(true);
  };

  const openEditModal = (doc) => {
    setModalDocID(doc.id || doc._id || '');
    setModalDocBody(JSON.stringify(doc, null, 2));
    setShowModal(true);
  };

  const handleSave = async () => {
    try {
      const parsed = JSON.parse(modalDocBody);

      let url = `/rest/v1/db/${activeScope}/${activeCollection}`;
      let method = 'POST';

      if (modalDocID) {
        url = `/rest/v1/db/${activeScope}/${activeCollection}/${modalDocID}`;
        method = 'PUT';
      }

      const res = await fetch(url, {
        method: method,
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(parsed),
      });

      if (res.ok) {
        setShowModal(false);
        fetchDocuments(activeScope, activeCollection);
      } else {
        const errData = await res.json();
        alert(`Save failed: ${errData.error || 'Unknown error'}`);
      }
    } catch (err) {
      alert('Invalid JSON formatting: ' + err.message);
    }
  };

  const handleSaveOIDC = async (e) => {
    e.preventDefault();
    if (!oidcClientId || !oidcClientSecret) {
      alert('Please fill out Client ID and Client Secret');
      return;
    }

    try {
      const res = await fetch(`/rest/v1/db/auth/oauth_providers/${oidcProvider}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          id: oidcProvider,
          client_id: oidcClientId,
          client_secret: oidcClientSecret,
        }),
      });

      if (res.ok) {
        alert(`${oidcProvider.toUpperCase()} Provider configuration saved successfully.`);
      } else {
        alert('Failed to save OIDC configuration.');
      }
    } catch (err) {
      console.error(err);
    }
  };

  const handleSaveSAML = async (e) => {
    e.preventDefault();
    if (!samlTenant || !samlXmlMetadata) {
      alert('Please enter a Tenant ID and upload XML metadata.');
      return;
    }

    try {
      const res = await fetch(`/rest/v1/db/auth/saml_providers/${samlTenant}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          id: samlTenant,
          entity_id: samlEntityId,
          sso_url: samlSsoUrl,
          metadata_xml: samlXmlMetadata,
        }),
      });

      if (res.ok) {
        alert(`SAML configuration for tenant '${samlTenant}' saved successfully.`);
      } else {
        alert('Failed to save SAML configuration.');
      }
    } catch (err) {
      console.error(err);
    }
  };

  const handleFileUpload = (e) => {
    const file = e.target.files[0];
    if (!file) return;

    const reader = new FileReader();
    reader.onload = (evt) => {
      setSamlXmlMetadata(evt.target.result);
    };
    reader.readAsText(file);
  };

  // Filter users by search input locally
  const filteredUsers = users.filter((u) => {
    const query = userSearch.toLowerCase();
    return (
      (u.email && u.email.toLowerCase().includes(query)) ||
      (u.role && u.role.toLowerCase().includes(query)) ||
      (u.id && u.id.toLowerCase().includes(query))
    );
  });

  return (
    <div className="app-container">
      {/* Sidebar Navigation */}
      <div className="sidebar">
        <div className="logo-section">
          <div className="logo-text">Couchbase Platform</div>
        </div>
        
        {/* Navigation Tabs Selector */}
        <div className="tab-navigation">
          <button
            className={`tab-button ${activeTab === 'database' ? 'active' : ''}`}
            onClick={() => setActiveTab('database')}
          >
            🗄️ Database
          </button>
          <button
            className={`tab-button ${activeTab === 'auth' ? 'active' : ''}`}
            onClick={() => setActiveTab('auth')}
          >
            🔑 Auth & Users
          </button>
        </div>

        {activeTab === 'database' && (
          <div className="schema-explorer">
            <div className="explorer-title">Database Explorer</div>
            {scopes.map((scope) => (
              <div className="scope-item" key={scope.name}>
                <div className="scope-header">
                  📁 <span>{scope.name}</span>
                </div>
                <div className="collections-list">
                  {scope.collections.map((col) => (
                    <div
                      className={`collection-item ${
                        activeScope === scope.name && activeCollection === col
                          ? 'active'
                          : ''
                      }`}
                      key={col}
                      onClick={() => {
                        setActiveScope(scope.name);
                        setActiveCollection(col);
                      }}
                    >
                      📄 {col}
                    </div>
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Main Panel Content */}
      <div className="main-panel">
        {activeTab === 'database' ? (
          <>
            <div className="header-panel">
              <div>
                <span className="breadcrumb-scope">{activeScope}</span>
                <span style={{ margin: '0 8px', color: 'var(--text-muted)' }}>/</span>
                <span className="breadcrumb-collection">{activeCollection}</span>
              </div>
              <div>
                <button className="btn-primary" onClick={openCreateModal}>
                  + Add Document
                </button>
              </div>
            </div>

            <div className="content-panel">
              {error && <div style={{ color: '#ef4444', marginBottom: 20 }}>{error}</div>}

              <div className="grid-container">
                <div className="table-card">
                  <div className="table-header">
                    <div className="card-title">Documents Grid</div>
                    <div style={{ fontSize: '0.8125rem', color: 'var(--text-muted)' }}>
                      Total rows: {documents.length}
                    </div>
                  </div>

                  {loading ? (
                    <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>
                      Loading documents...
                    </div>
                  ) : (
                    <table className="data-table">
                      <thead>
                        <tr>
                          <th style={{ width: '25%' }}>Document ID</th>
                          <th style={{ width: '60%' }}>Document Fields Preview</th>
                          <th style={{ width: '15%', textAlign: 'right' }}>Actions</th>
                        </tr>
                      </thead>
                      <tbody>
                        {documents.length === 0 ? (
                          <tr>
                            <td colSpan="3" style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>
                              No documents found inside this collection.
                            </td>
                          </tr>
                        ) : (
                          documents.map((doc, idx) => {
                            const docId = doc.id || doc._id || `row_${idx}`;
                            return (
                              <tr key={docId}>
                                <td className="doc-id-cell">{docId}</td>
                                <td>
                                  <div className="doc-preview">
                                    {JSON.stringify(doc)}
                                  </div>
                                </td>
                                <td style={{ textAlign: 'right', display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
                                  <button
                                    className="btn-secondary"
                                    style={{ padding: '4px 8px', fontSize: '0.75rem' }}
                                    onClick={() => openEditModal(doc)}
                                  >
                                    Edit
                                  </button>
                                  <button
                                    className="btn-danger"
                                    style={{ padding: '4px 8px', fontSize: '0.75rem' }}
                                    onClick={() => handleDelete(docId)}
                                  >
                                    Delete
                                  </button>
                                </td>
                              </tr>
                            );
                          })
                        )}
                      </tbody>
                    </table>
                  )}
                </div>
              </div>
            </div>
          </>
        ) : (
          /* Authentication Manager Tab */
          <>
            <div className="header-panel">
              <div>
                <span className="breadcrumb-collection">Authentication & Identity Manager</span>
              </div>
            </div>

            <div className="content-panel">
              {/* Users Directory Table */}
              <div className="table-card">
                <div className="table-header">
                  <div className="card-title">Registered Accounts Directory</div>
                  <div>
                    <input
                      type="text"
                      className="form-input"
                      placeholder="Search accounts..."
                      style={{ width: 220, padding: '6px 10px', fontSize: '0.8125rem' }}
                      value={userSearch}
                      onChange={(e) => setUserSearch(e.target.value)}
                    />
                  </div>
                </div>

                {loading ? (
                  <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-muted)' }}>
                    Loading user directory...
                  </div>
                ) : (
                  <table className="data-table">
                    <thead>
                      <tr>
                        <th style={{ width: '30%' }}>User ID</th>
                        <th style={{ width: '40%' }}>Email Address</th>
                        <th style={{ width: '30%' }}>Role</th>
                      </tr>
                    </thead>
                    <tbody>
                      {filteredUsers.length === 0 ? (
                        <tr>
                          <td colSpan="3" style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>
                            No matching user accounts registered.
                          </td>
                        </tr>
                      ) : (
                        filteredUsers.map((u, idx) => (
                          <tr key={u.id || idx}>
                            <td className="doc-id-cell">{u.id || 'N/A'}</td>
                            <td>{u.email || 'N/A'}</td>
                            <td style={{ color: 'var(--accent-cyan)', fontWeight: 600 }}>
                              {u.role || 'user'}
                            </td>
                          </tr>
                        ))
                      )}
                    </tbody>
                  </table>
                )}
              </div>

              {/* OIDC & SAML SSO Form Controls Grid */}
              <div className="settings-grid">
                {/* OIDC Configuration Card */}
                <div className="settings-card">
                  <div className="settings-title">OIDC Client Config Setup</div>
                  <form onSubmit={handleSaveOIDC}>
                    <div className="form-group">
                      <label className="form-label">Identity Provider</label>
                      <select
                        className="form-input"
                        value={oidcProvider}
                        onChange={(e) => setOidcProvider(e.target.value)}
                      >
                        <option value="google">Google Social Auth</option>
                        <option value="github">GitHub OAuth Gateway</option>
                      </select>
                    </div>
                    <div className="form-group">
                      <label className="form-label">Client ID</label>
                      <input
                        type="text"
                        className="form-input"
                        placeholder="Enter Client ID credentials"
                        value={oidcClientId}
                        onChange={(e) => setOidcClientId(e.target.value)}
                      />
                    </div>
                    <div className="form-group">
                      <label className="form-label">Client Secret</label>
                      <input
                        type="password"
                        className="form-input"
                        placeholder="Enter Client Secret credentials"
                        value={oidcClientSecret}
                        onChange={(e) => setOidcClientSecret(e.target.value)}
                      />
                    </div>
                    <button type="submit" className="btn-primary" style={{ width: '100%' }}>
                      Save OIDC Configuration
                    </button>
                  </form>
                </div>

                {/* SAML Enterprise Configuration Card */}
                <div className="settings-card">
                  <div className="settings-title">SAML Enterprise SSO config</div>
                  <form onSubmit={handleSaveSAML}>
                    <div className="form-group">
                      <label className="form-label">Tenant Identifier</label>
                      <input
                        type="text"
                        className="form-input"
                        placeholder="e.g. corporate"
                        value={samlTenant}
                        onChange={(e) => setSamlTenant(e.target.value)}
                      />
                    </div>
                    <div className="form-group">
                      <label className="form-label">Provider Entity ID (Optional)</label>
                      <input
                        type="text"
                        className="form-input"
                        placeholder="e.g. https://idp.example.com"
                        value={samlEntityId}
                        onChange={(e) => setSamlEntityId(e.target.value)}
                      />
                    </div>
                    <div className="form-group">
                      <label className="form-label">Single Sign-On URL (Optional)</label>
                      <input
                        type="text"
                        className="form-input"
                        placeholder="e.g. https://idp.example.com/sso"
                        value={samlSsoUrl}
                        onChange={(e) => setSamlSsoUrl(e.target.value)}
                      />
                    </div>
                    <div className="form-group">
                      <label className="form-label">Upload IdP Metadata XML File</label>
                      <input
                        type="file"
                        className="form-input"
                        accept=".xml"
                        onChange={handleFileUpload}
                      />
                    </div>
                    {samlXmlMetadata && (
                      <div className="form-group">
                        <label className="form-label">Loaded XML Metadata (Preview)</label>
                        <textarea
                          className="form-textarea"
                          style={{ height: 100 }}
                          value={samlXmlMetadata}
                          readOnly
                        />
                      </div>
                    )}
                    <button type="submit" className="btn-primary" style={{ width: '100%' }}>
                      Save SAML Configuration
                    </button>
                  </form>
                </div>
              </div>
            </div>
          </>
        )}
      </div>

      {/* Editor Modal */}
      {showModal && (
        <div className="modal-overlay">
          <div className="modal-content">
            <div className="modal-header">
              <div style={{ fontWeight: 600 }}>
                {modalDocID ? `Edit Document: ${modalDocID}` : 'Create New Document'}
              </div>
              <button
                style={{ background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer', fontSize: '1.25rem' }}
                onClick={() => setShowModal(false)}
              >
                &times;
              </button>
            </div>
            <div className="modal-body">
              {!modalDocID && (
                <div className="form-group">
                  <label className="form-label">Document ID (optional)</label>
                  <input
                    type="text"
                    className="form-input"
                    placeholder="Generates UUID if left empty"
                    value={modalDocID}
                    onChange={(e) => setModalDocID(e.target.value)}
                  />
                </div>
              )}
              <div className="form-group">
                <label className="form-label">Document JSON Payload</label>
                <textarea
                  className="form-textarea"
                  value={modalDocBody}
                  onChange={(e) => setModalDocBody(e.target.value)}
                />
              </div>
            </div>
            <div className="modal-footer">
              <button className="btn-secondary" onClick={() => setShowModal(false)}>
                Cancel
              </button>
              <button className="btn-primary" onClick={handleSave}>
                Save Changes
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default App;
