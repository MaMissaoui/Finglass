package pdftext

// matrix is a 2D affine transform laid out exactly like euclid's
// Transform2D for row vectors. A point is transformed as x' = x*m11 +
// y*m21 + m31 and y' = x*m12 + y*m22 + m32.
type matrix struct {
	m11, m12 float64
	m21, m22 float64
	m31, m32 float64
}

func identityMatrix() matrix {
	return matrix{m11: 1, m22: 1}
}

// rowMajor builds a transform with euclid's row_major argument order.
func rowMajor(m11, m12, m21, m22, m31, m32 float64) matrix {
	return matrix{m11: m11, m12: m12, m21: m21, m22: m22, m31: m31, m32: m32}
}

func translation(tx, ty float64) matrix {
	return rowMajor(1, 0, 0, 1, tx, ty)
}

// postTransform returns m × mat, so mat applies after m. The expression
// order mirrors euclid 0.20's post_transform.
func (m matrix) postTransform(mat matrix) matrix {
	return matrix{
		m11: m.m11*mat.m11 + m.m12*mat.m21,
		m12: m.m11*mat.m12 + m.m12*mat.m22,
		m21: m.m21*mat.m11 + m.m22*mat.m21,
		m22: m.m21*mat.m12 + m.m22*mat.m22,
		m31: m.m31*mat.m11 + m.m32*mat.m21 + mat.m31,
		m32: m.m31*mat.m12 + m.m32*mat.m22 + mat.m32,
	}
}

// preTransform returns mat × m, so mat applies before m.
func (m matrix) preTransform(mat matrix) matrix {
	return mat.postTransform(m)
}

func (m matrix) transformVector(x, y float64) (float64, float64) {
	return x*m.m11 + y*m.m21, x*m.m12 + y*m.m22
}
