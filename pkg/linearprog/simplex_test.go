package linearprog

import (
	. "github.com/smartystreets/goconvey/convey"
	"testing"
)

func TestSimplex(t *testing.T) {
	Convey("Given the following matrix maximization problem 1 ...", t, func() {
		a := []float64{40, 60}
		b := []float64{70, 40, 90}
		constdir := []string{"<=", "<=", "<="}
		A := map[int]map[int]float64{
			1: map[int]float64{1: 2, 2: 1},
			2: map[int]float64{1: 1, 2: 1},
			3: map[int]float64{1: 1, 2: 3},
		}
		Convey("The solutions to 1 should be ... ", func() {
			solutions, _ := Simplex(A, b, a, constdir)
			out := map[int]float64{0: 15, 1: 25}
			So(solutions, ShouldResemble, out)
		})
	})

	Convey("Given the following matrix maximization problem 2 ... ", t, func() {
		a := []float64{3, 2, 5}
		b := []float64{430, 460, 420}
		constdir := []string{"<=", "<=", "<="}
		A := map[int]map[int]float64{
			1: map[int]float64{1: 1, 2: 2, 3: 1},
			2: map[int]float64{1: 3, 2: 0, 3: 2},
			3: map[int]float64{1: 1, 2: 4, 3: 0},
		}
		Convey("The solutions to 2 should be ... ", func() {
			solutions, _ := Simplex(A, b, a, constdir)
			out := map[int]float64{0: 0, 1: 100, 2: 230}
			So(solutions, ShouldResemble, out)
		})
	})

	Convey("Given the following matrix maximization problem 3 ...", t, func() {
		a := []float64{3, 2}
		b := []float64{18, 42, 24}
		constdir := []string{"<=", "<=", "<="}
		A := map[int]map[int]float64{
			1: map[int]float64{1: 2, 2: 1},
			2: map[int]float64{1: 2, 2: 3},
			3: map[int]float64{1: 3, 2: 1},
		}
		Convey("The solutions to 3 should be ... ", func() {
			solutions, _ := Simplex(A, b, a, constdir)
			out := map[int]float64{0: 3, 1: 12}
			So(solutions, ShouldResemble, out)
		})
	})

	Convey("Given the following matrix maximization problem 4 ...", t, func() {
		a := []float64{2, 3, 10, 5, 3}
		b := []float64{425, 25, 120, 6, 1}
		constdir := []string{"<=", "<=", "<=", "<=", "<="}
		A := map[int]map[int]float64{
			1: map[int]float64{1: 1, 2: 1, 3: 1, 4: 3, 5: 2},
			2: map[int]float64{1: 2, 2: -4, 3: 1, 4: 0, 5: 0},
			3: map[int]float64{1: -1, 2: -1, 3: -3, 4: 0, 5: 0},
			4: map[int]float64{1: 1, 2: 11, 3: 5, 4: 0, 5: 0},
			5: map[int]float64{1: 1, 2: 1, 3: 7, 4: 0, 5: 0},
		}
		Convey("The solutions to 4 should be ... ", func() {
			solutions, _ := Simplex(A, b, a, constdir)
			out := map[int]float64{0: 0, 1: 0.51, 2: 0.07, 3: 141.47, 4: 0}
			So(solutions, ShouldResemble, out)
		})
	})

	Convey("Given the following matrix maximization problem 5 ...", t, func() {
		a := []float64{225000, 18000, 92000}
		b := []float64{17000, 1.25, 1.25, 1.25, 1.0, 0.8, 0.1}
		constdir := []string{"<=", "<=", "<=", "<=", ">=", ">=", ">="}
		A := map[int]map[int]float64{
			1: map[int]float64{1: 11000, 2: 988, 3: 5000},
			2: map[int]float64{1: 1, 2: 0, 3: 0},
			3: map[int]float64{1: 0, 2: 1, 3: 0},
			4: map[int]float64{1: 0, 2: 0, 3: 1},
			5: map[int]float64{1: 1, 2: 0, 3: 0},
			6: map[int]float64{1: 0, 2: 1, 3: 0},
			7: map[int]float64{1: 0, 2: 0, 3: 1},
		}
		Convey("The solutions to 5 should be ... ", func() {
			solutions, _ := Simplex(A, b, a, constdir)
			out := map[int]float64{0: 1.25, 1: 0.8, 2: 0.49}
			So(solutions, ShouldResemble, out)
		})
	})

	Convey("Given the following matrix maximization problem 6 ...", t, func() {
		a := []float64{1, 1, 2}
		b := []float64{50, 36, 10}
		constdir := []string{"<=", ">=", ">="}
		A := map[int]map[int]float64{
			1: map[int]float64{1: 2, 2: 1, 3: 1},
			2: map[int]float64{1: 2, 2: 1, 3: 0},
			3: map[int]float64{1: 1, 2: 0, 3: 1},
		}
		Convey("The solutions to 3 should be ... ", func() {
			solutions, _ := Simplex(A, b, a, constdir)
			out := map[int]float64{0: 0, 1: 36, 2: 14}
			So(solutions, ShouldResemble, out)
		})
	})

	Convey("Given the following matrix maximization problem 7 ...", t, func() {
		a := []float64{1450, 1450, 1450, 3950}
		b := []float64{40064, 1.25, 1.25, 1.25, 1.25, 0.96, 0.68, 0.39, 0.1}
		constdir := []string{"<=", "<=", "<=", "<=", "<=", ">=", ">=", ">=", ">="}
		A := map[int]map[int]float64{
			1: map[int]float64{1: 1430.86, 2: 1430.86, 3: 1430.86, 4: 1430.86},
			2: map[int]float64{1: 1, 2: 0, 3: 0, 4: 0},
			3: map[int]float64{1: 0, 2: 1, 3: 0, 4: 0},
			4: map[int]float64{1: 0, 2: 0, 3: 1, 4: 0},
			5: map[int]float64{1: 0, 2: 0, 3: 0, 4: 1},
			6: map[int]float64{1: 1, 2: 0, 3: 0, 4: 0},
			7: map[int]float64{1: 0, 2: 1, 3: 0, 4: 0},
			8: map[int]float64{1: 0, 2: 0, 3: 1, 4: 0},
			9: map[int]float64{1: 0, 2: 0, 3: 0, 4: 1},
		}
		Convey("The solutions to 7 should be ... ", func() {
			solutions, _ := Simplex(A, b, a, constdir)
			out := map[int]float64{0: 1.25, 1: 1.25, 2: 1.25, 3: 1.25}
			So(solutions, ShouldResemble, out)
		})
	})

	Convey("Given the following matrix maximization problem 8 ...", t, func() {
		a := []float64{1, 24, 10}
		b := []float64{84.75, 2.5, 3.5, 0.5}
		constdir := []string{"=", "<=", "<=", "<="}
		A := map[int]map[int]float64{
			1: map[int]float64{1: 5, 2: 25, 3: 0.5},
			2: map[int]float64{1: 1, 2: 0, 3: 0},
			3: map[int]float64{1: 0, 2: 1, 3: 0},
			4: map[int]float64{1: 0, 2: 0, 3: 1},
		}
		Convey("The solutions to 3 should be ... ", func() {
			solutions, _ := Simplex(A, b, a, constdir)
			out := map[int]float64{0: 0, 1: 3.38, 2: 0.5}
			So(solutions, ShouldResemble, out)
		})
	})

	Convey("Given the following matrix maximization problem 9 ...", t, func() {
		a := []float64{7178.93, 35.4284, 2.630216}
		b := []float64{37.90, 0, 13.16836408, 16.69836408}
		constdir := []string{"=", "<=", "<=", "<="}
		A := map[int]map[int]float64{
			1: map[int]float64{1: 713.3101, 2: 71.9862, 3: 29.04887},
			2: map[int]float64{1: 1, 2: 0, 3: 0},
			3: map[int]float64{1: 0, 2: 1, 3: 0},
			4: map[int]float64{1: 0, 2: 0, 3: 1},
		}
		Convey("The solutions to 3 should be ... ", func() {
			solutions, _ := Simplex(A, b, a, constdir)
			out := map[int]float64{0: 0, 1: 0.53, 2: 0}
			So(solutions, ShouldResemble, out)
		})
	})

}
