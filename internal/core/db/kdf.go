package db

import dbkdf "github.com/MaMissaoui/Finglass/internal/core/db/kdf"

// DefaultKDFIterations is SQLCipher 4's default PBKDF2 iteration count (256000).
//
// Production relies on this SQLCipher default and never sets it. Only the
// finglass_testkdf build lowers it for tests that read fixtures written by
// production builds (see internal/core/db/kdftest).
const DefaultKDFIterations = dbkdf.DefaultKDFIterations
