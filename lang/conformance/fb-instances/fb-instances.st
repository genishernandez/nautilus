PROGRAM InstSt
(* Two CTU instances in Structured Text, outputs read as inst.Pin. *)
VAR_EXTERNAL
    CU1    : BOOL;
    CU2    : BOOL;
    PV     : INT;
    st_Q1  : BOOL;
    st_CV1 : INT;
    st_Q2  : BOOL;
    st_CV2 : INT;
END_VAR
VAR
    c1 : CTU;
    c2 : CTU;
END_VAR

c1(CU := CU1, PV := PV);
c2(CU := CU2, PV := PV);
st_Q1  := c1.Q;
st_CV1 := c1.CV;
st_Q2  := c2.Q;
st_CV2 := c2.CV;
END_PROGRAM
