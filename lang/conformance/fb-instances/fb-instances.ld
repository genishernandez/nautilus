PROGRAM InstLd
(* Two CTU instances in ladder: each rung's power drives one instance's CU;
   later rungs read the instances' pins as inst.Pin (Q as a contact, CV in
   an assignment). *)
VAR_EXTERNAL
    CU1    : BOOL;
    CU2    : BOOL;
    PV     : INT;
    ld_Q1  : BOOL;
    ld_CV1 : INT;
    ld_Q2  : BOOL;
    ld_CV2 : INT;
END_VAR
VAR
    c1 : CTU;
    c2 : CTU;
END_VAR
LD
  RUNG count1
    CU1 c1:CTU(PV := PV)
  RUNG count2
    CU2 c2:CTU(PV := PV)
  RUNG read1
    c1.Q ( ld_Q1 )
  RUNG read2
    c2.Q ( ld_Q2 )
  RUNG values
    { ld_CV1 := c1.CV; ld_CV2 := c2.CV }
END_LD
END_PROGRAM
