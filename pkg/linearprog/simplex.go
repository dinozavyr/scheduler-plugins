package linearprog

import (
	"math"
	"runtime"
)

type Parameter struct {
	LHS       map[int]map[int]float64 // Left hand side constraint.
	RHS       []float64               // Right hand side constraint.
	ObjFun    []float64               // Objective functions.
	Constdir  []string                // Constraint directions.
	Pivot     map[int]map[int]float64 // Pivot Matrix.
	Nrows     int                     // Rows number pivot matrix.
	Ncols     int                     // Columns number of pivot matrix.
	Nproc     int                     // Number of cpu cores.
	Ndiv      float64                 // Divisor of the partitions.
	Rowsids   [][]int
	Cpivot    int // Columns Pivot2
	Rpivot    int // Row Pivot
	Feasible  bool
	Npositive int
}

func Simplex(A map[int]map[int]float64, b []float64, a []float64, constdir []string) (map[int]float64, float64) {

	par := &Parameter{
		ObjFun:   a,
		LHS:      A,
		RHS:      b,
		Constdir: constdir,
		Nrows:    len(b) + 1,
		Ncols:    len(a) + len(b) + 2,
	}

	par.InitPivot()
	par.Partition()

	for {
		par.ColPivot()
		par.RowPivot()
		par.PutOneInPivot()
		par.PutZerosInColPivot()
		par.CheckFeasibility()
		if par.Npositive == par.Ncols {
			break
		}
	}

	solution, opt := par.GetSolution()

	return solution, opt
}

func (par *Parameter) InitPivot() {
	Pivot := make(map[int]map[int]float64)
	Pivot[0] = map[int]float64{0: 1}
	la := len(par.ObjFun)
	for i := 0; i < par.Nrows; i++ {
		if Pivot[i] == nil {
			Pivot[i] = map[int]float64{}
		}
		for j := 0; j < par.Ncols; j++ {
			if i == 0 && j >= 1 && j <= la {
				Pivot[i][j] = -par.ObjFun[j-1]
			} else if i > 0 && j >= 1 && j <= la {
				Pivot[i][j] = par.LHS[i][j]
			} else if i > 0 && j > la && j < par.Ncols-1 && par.Constdir[i-1] == "<=" {
				Pivot[i][i+la] = 1.00
			} else if i > 0 && j > la && j < par.Ncols-1 && par.Constdir[i-1] == ">=" {
				Pivot[i][i+la] = -1.00
			} else if i > 0 && j == par.Ncols-1 {
				Pivot[i][j] = par.RHS[i-1]
			}
		}
	}
	par.Pivot = Pivot
}

func (par *Parameter) Partition() {
	par.Nproc = runtime.NumCPU()
	if par.Nrows >= par.Nproc {
		par.Ndiv = math.Floor(float64(par.Nrows) / float64(par.Nproc))
	} else {
		par.Ndiv = float64(par.Nrows) / float64(par.Nproc)
	}

	r := 0.00
	var ids []int
	s1, s2 := 0.00, 1.00

	for i := range par.Pivot {
		if s1 < (s2*par.Ndiv + r) {
			ids = append(ids, int(i))
			if s1 == ((float64(par.Nproc) * par.Ndiv) + r - 1) {
				par.Rowsids = append(par.Rowsids, ids)
			}
		} else {
			s2++
			if s2 == float64(par.Nproc) {
				r = math.Mod(float64(len(par.Pivot)), float64(par.Nproc))
			}
			par.Rowsids = append(par.Rowsids, ids)
			ids = []int{i}
			if s1 == ((float64(par.Nproc) * par.Ndiv) + r - 1) {
				par.Rowsids = append(par.Rowsids, ids)
			}
		}
		s1++
	}

	if len(par.Rowsids) < par.Nproc {
		par.Nproc = len(par.Rowsids)
	}
}

func (par *Parameter) ColPivot() {
	par.Cpivot = 1
	min := par.Pivot[0][1]
	for i := 1; i < par.Ncols; i++ {
		if par.Feasible {
			if par.Pivot[0][i] < 0 && min > par.Pivot[0][i] {
				par.Cpivot = i
				min = par.Pivot[0][i]
			}
		} else {
			if par.Pivot[0][i] < 0 && min > par.Pivot[0][i] && i <= len(par.ObjFun) {
				par.Cpivot = i
				min = par.Pivot[0][i]
			}
		}
	}
}

func (par *Parameter) RowPivot() {
	min := math.Inf(1)
	par.Rpivot = 1
	for k := 1; k < par.Nrows; k++ {
		if par.Pivot[k][par.Cpivot] > 0 {
			v := par.Pivot[k][par.Ncols-1] / par.Pivot[k][par.Cpivot]
			if v >= 0 {
				if min > v {
					min = v
					par.Rpivot = k
				}
			}
		}
	}
}

func (par *Parameter) PutOneInPivot() {
	elementpivot := par.Pivot[par.Rpivot][par.Cpivot]
	for j := 0; j < par.Ncols; j++ {
		par.Pivot[par.Rpivot][j] = par.Pivot[par.Rpivot][j] / elementpivot
	}
}

func (par *Parameter) PutZerosInColPivot() {
	c := make(chan map[int]map[int]float64)
	for i := 0; i < par.Nproc; i++ {
		go DifferenceRows(par.Pivot, par.Rowsids[i], par.Cpivot, par.Rpivot, par.Ncols, c)
	}

	u := make([]map[int]map[int]float64, par.Nproc)
	for i := 0; i < par.Nproc; i++ {
		u[i] = <-c
	}

	for i := 0; i < par.Nproc; i++ {
		for k := range u[i] {
			par.Pivot[k] = u[i][k]
		}
	}
}

func DifferenceRows(Pivot map[int]map[int]float64, rowsids []int, colpivot, rowspivot, columns int, c chan map[int]map[int]float64) {
	Pivot2 := make(map[int]map[int]float64)
	numberrows := len(rowsids)
	for i := 0; i < numberrows; i++ {
		lowervalue := Pivot[rowsids[i]][colpivot]
		if rowsids[i] != rowspivot && lowervalue != 0 {
			for j := 0; j < columns; j++ {
				if Pivot2[rowsids[i]] == nil {
					Pivot2[rowsids[i]] = map[int]float64{}
				}
				Pivot2[rowsids[i]][j] = (-lowervalue)*(Pivot[rowspivot][j]) + Pivot[rowsids[i]][j]
			}
		}
	}
	c <- Pivot2
}

func (par *Parameter) CheckFeasibility() {
	par.Npositive = 0
	numberfeasible := 0
	for i := 0; i < par.Ncols; i++ {
		if par.Pivot[0][i] >= 0 {
			par.Npositive = par.Npositive + 1
		}
		if par.Pivot[0][i] >= 0 && i < len(par.ObjFun) {
			numberfeasible = numberfeasible + 1
		}
	}

	if numberfeasible == len(par.ObjFun) {
		par.Feasible = true
	}
}

func (par *Parameter) GetSolution() (map[int]float64, float64) {
	solutions := make(map[int]float64)
	opt := par.Pivot[0][par.Ncols-1]
	la, lb := len(par.ObjFun), len(par.RHS)
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			switch {
			case par.Pivot[0][i] > 0:
				solutions[i-1] = 0
			case par.Pivot[0][i] == 0 && par.Pivot[j][i] == 1:
				solutions[i-1] = Round(par.Pivot[j][par.Ncols-1], 2)
			}
		}
	}
	return solutions, opt
}

func Round(number float64, precision int) float64 {
	var out float64
	v1 := math.Pow(10, float64(precision))
	v2 := int(number*v1 + math.Copysign(0.5, number*v1))
	out = float64(v2) / v1
	return out
}
