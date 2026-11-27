// Exercise native validation on a real form with optional advanced fields.
(() => {
  const results = [];
  const check = (name, condition) => results.push({name, passed: Boolean(condition)});
  const name = document.querySelector('input[name="name"]');
  const form = name.form;
  const details = name.closest("details");
  const submit = form.querySelector('button[type="submit"]');
  let submissions = 0;
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    submissions++;
  });
  form.querySelector('input[name="label"]').value = "Browser validation probe";
  details.open = true;
  name.value = "INVALID TYPE";
  details.open = false;
  submit.click();
  check("invalid advanced fields are revealed before native validation focuses them", details.open && document.activeElement === name);
  check("invalid advanced fields still prevent submission", submissions === 0 && name.validity.patternMismatch);
  name.value = "browser_probe";
  details.open = false;
  submit.click();
  check("corrected advanced fields submit without unnecessarily opening the disclosure", submissions === 1 && !details.open);
  return results;
})()
