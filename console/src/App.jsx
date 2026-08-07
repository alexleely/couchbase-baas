import React, { useState, useEffect } from 'react';
import './App.css';

function App() {
  const [scopes, setScopes] = useState([]);
  const [activeScope, setActiveScope] = useState('');
  const [activeCollection, setActiveCollection] = useState('');
  const [documents, setDocuments] = useState([]);
  const [showModal, setShowModal] = useState(false);
  const [modalDocID, setModalDocID] = useState('');
  const [modalDocBody, setModalDocBody] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  // 1. Fetch DB Schema on load
  useEffect(() => {
    fetchSchema();
  }, []);

  // 2. Fetch Documents when active collection changes
  useEffect(() => {
    if (activeScope && activeCollection) {
      fetchDocuments(activeScope, activeCollection);
    }
  }, [activeScope, activeCollection]);

  const fetchSchema = async () => {
    try {
      const res = await fetch('/rest/v1/db/schema');
      const data = await res.json();
      if (data.scopes && data.scopes.length > 0) {
        setScopes(data.scopes);
        // Default to first collection
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
      // Validate JSON syntax
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

  return (
    <div className="app-container">
      {/* Sidebar Scope Tree */}
      <div className="sidebar">
        <div className="logo-section">
          <div className="logo-text">Couchbase Platform</div>
        </div>
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
      </div>

      {/* Main Panel Content */}
      <div className="main-panel">
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
