PROGRAM CtuSt
(* CTU in Structured Text: the reference path. *)
VAR_EXTERNAL
    CU    : BOOL;
    R     : BOOL;
    PV    : INT;
    st_Q  : BOOL;
    st_CV : INT;
END_VAR
VAR
    c : CTU;
END_VAR

c(CU := CU, R := R, PV := PV);
st_Q  := c.Q;
st_CV := c.CV;
END_PROGRAM
