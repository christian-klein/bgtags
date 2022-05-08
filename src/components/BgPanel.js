import { useState } from 'react';
import QRCode from "react-qr-code";
import Backdrop from './Backdrop';
import Modal from './Modal';

function BgPanel(props) {
    var pathToRules = window.location.origin + props.rulesUrl;

    const [modalIsOpen, setModalIsOpen] = useState(false);

    function deleteHandler() {
        setModalIsOpen(true);
    }

    function closeHandler() {
        setModalIsOpen(false);
    }

    return (
        <div className="card">
            <h2>{props.title}</h2>
            <a href={pathToRules}>
                <img src={props.image} alt={props.title} className="imgCard" />
                <QRCode value={pathToRules} />
            </a>
            <div className="actions">
                <button className="btn" onClick={deleteHandler}>Delete</button>
            </div>


            {modalIsOpen && <Modal onCancel={closeHandler} onConfirm={closeHandler} />}
            {modalIsOpen && <Backdrop onClick={closeHandler} />}

        </div>
    );
}

export default BgPanel;