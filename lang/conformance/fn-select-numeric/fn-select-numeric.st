PROGRAM SelectNumericSt
(* Selection and numeric standard functions, positional and formal. The
   INT outputs take ABS of an INT and TRUNC's result without a conversion,
   so each compiles only because the result is an integer (a REAL would be
   "cannot assign REAL to INT"). *)
VAR_EXTERNAL
    G : BOOL;
    K, X, Y, Z : INT;
    D : DINT;
    R, BIG : REAL;
    st_SELP, st_SELF, st_MUXF, st_MIN3, st_MAX3, st_MINF, st_ABSI : INT;
    st_MAX4 : DINT;
    st_ABSD : DINT;
    st_ABSR, st_ABSF : REAL;
    st_TRUNC : LINT;
END_VAR

st_SELP := SEL(G, X, Y);
st_SELF := SEL(G := G, IN0 := X, IN1 := Y);
st_MUXF := MUX(K := K, IN0 := X, IN1 := Y, IN2 := Z);
st_MIN3 := MIN(X, Y, Z);
st_MAX3 := MAX(X, Y, Z);
st_MINF := MIN(IN1 := X, IN2 := Y, IN3 := Z);
st_MAX4 := MAX(X, Y, Z, D);
st_ABSI := ABS(X);
st_ABSD := ABS(D);
st_ABSR := ABS(R);
st_ABSF := ABS(IN := R);
st_TRUNC := TRUNC(BIG);
END_PROGRAM
