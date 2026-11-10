// The assistant's page only paints what the app says (a Vista, already in
// the person's language) and tells it which button was pressed.
(function () {
  var el = function (id) { return document.getElementById(id); };
  var accion = "";

  function poner(id, texto) {
    el(id).textContent = texto || "";
    el(id).hidden = !texto;
  }

  function pintar(v) {
    if (!v) return;
    document.documentElement.lang = v.idioma;
    accion = v.accion;
    var pasos = el("pasos");
    pasos.textContent = "";
    (v.pasos || []).forEach(function (p) {
      var li = document.createElement("li");
      li.textContent = p.nombre;
      li.className = p.estado;
      pasos.appendChild(li);
    });
    poner("titulo", v.titulo);
    poner("texto", v.texto);
    poner("explicacion", v.problema);
    poner("detalle", v.detalle);
    el("problema").hidden = !v.problema;
    poner("reloj", v.reloj);
    poner("rotulo", v.campo);
    el("campo").hidden = !v.campo;
    poner("boton", v.boton);
    poner("revisar", v.revisar);
    poner("version", v.version);
    // A progress bar without a value moves by itself: the app is working
    // and cannot say how far it is.
    el("avance").hidden = !v.ocupado;
    if (v.avance >= 0) el("avance").value = v.avance;
    else el("avance").removeAttribute("value");
    if (v.campo && document.activeElement !== el("valor")) el("valor").focus();
  }

  function llamar(metodo, valor) {
    var a = window.go && window.go.main && window.go.main.Asistente;
    if (!a || !a[metodo]) return;
    var hecho = valor === undefined ? a[metodo]() : a[metodo](valor);
    if (hecho && hecho.catch) hecho.catch(function () {});
    return hecho;
  }

  el("forma").addEventListener("submit", function (e) {
    e.preventDefault();
    if (!accion) return;
    if (el("campo").hidden) llamar(accion);
    else if (el("valor").value.trim()) llamar(accion, el("valor").value.trim());
  });
  el("revisar").addEventListener("click", function () { llamar("Revisar"); });

  // Listen first, then ask: a change between the two is not lost.
  if (window.runtime) window.runtime.EventsOn("vista", pintar);
  var primera = llamar("Vista");
  if (primera) primera.then(pintar);
})();
