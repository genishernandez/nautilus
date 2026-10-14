PROGRAM CtuLd
(* CTU in ladder: rung power drives CU, R and PV are passed by name, Q
   continues to the coil, CV is captured with the standard's output
   binding. *)
VAR_EXTERNAL
    CU    : BOOL;
    R     : BOOL;
    PV    : INT;
    ld_Q  : BOOL;
    ld_CV : INT;
END_VAR
VAR
    c : CTU;
END_VAR
LD
  RUNG ctu
    CU c:CTU(R := R, PV := PV, CV => ld_CV) ( ld_Q )
END_LD
END_PROGRAM
