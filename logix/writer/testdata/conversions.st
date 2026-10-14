PROGRAM Conversions
(* Every conversion the Logix writer writes (#255); what it refuses is in
   convert_test.go. *)
VAR
    d : DINT;
    a : DINT;
    i : INT;
    s : SINT;
    r : REAL;
    lr : LREAL;
    b : BOOL;
    t : TIME;
    arr : ARRAY [0..3] OF INT;
END_VAR
(* integer narrowing wraps, in integer arithmetic *)
i := DINT_TO_INT(d);
s := DINT_TO_SINT(d);
s := INT_TO_SINT(i);
i := DINT_TO_INT(70000);
d := DINT_TO_INT(a + d) * 2;
(* the argument is read as the source type *)
d := INT_TO_DINT(d);
d := INT_TO_DINT(a + d);
(* widening is the value itself *)
d := INT_TO_DINT(i);
d := SINT_TO_DINT(s) + 1;
i := SINT_TO_INT(s);
d := TO_DINT(i);
i := TO_INT(d);
(* TIME is a DINT of milliseconds *)
d := TIME_TO_DINT(t);
i := TIME_TO_INT(t);
t := DINT_TO_TIME(d);
(* integer to REAL *)
r := DINT_TO_REAL(d);
r := r + INT_TO_REAL(i);
r := DINT_TO_REAL(a) / 2;
r := DINT_TO_REAL(a) / 2.0;
lr := DINT_TO_LREAL(d);
lr := REAL_TO_LREAL(r);
r := LREAL_TO_REAL(lr);
IF INT_TO_REAL(i) > r THEN
    b := TRUE;
END_IF;
(* to BOOL: nonzero *)
b := DINT_TO_BOOL(d);
b := INT_TO_BOOL(d);
b := REAL_TO_BOOL(r);
(* BOOL to a number: the IF it is *)
d := BOOL_TO_DINT(b);
r := BOOL_TO_REAL(b);
(* truncation stores a whole number *)
d := TRUNC_DINT(r);
(* a comparison and an index are integer computations of their own *)
IF DINT_TO_INT(d) > 5 AND r > 1.5 THEN
    r := r + arr[DINT_TO_SINT(d)];
END_IF;
END_PROGRAM
